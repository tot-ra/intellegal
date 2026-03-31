package handlers

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/models"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

func (a *API) UsePostgres(pg *db.Postgres) error {
	if pg == nil {
		return nil
	}
	a.pg = pg
	a.contractsModel = models.NewContracts(pg)
	a.documentsModel = models.NewDocuments(pg)
	return a.loadPersistedState(context.Background())
}

func (a *API) loadPersistedState(ctx context.Context) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}

	contracts, err := a.loadContracts(ctx, conn)
	if err != nil {
		return err
	}
	documents, err := a.loadDocuments(ctx, conn, contracts)
	if err != nil {
		return err
	}
	checks, err := a.loadChecks(ctx, conn)
	if err != nil {
		return err
	}
	idempotency, err := a.loadIdempotency(ctx, conn)
	if err != nil {
		return err
	}
	copyEvents, err := a.loadCopyEvents(ctx, conn)
	if err != nil {
		return err
	}

	a.mu.Lock()
	a.contracts = contracts
	a.documents = documents
	a.checks = checks
	a.idempotency = idempotency
	a.copyEvents = copyEvents
	a.mu.Unlock()
	return nil
}

func (a *API) loadContracts(ctx context.Context, conn *pgxpool.Pool) (map[string]contract, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, name, source_type, COALESCE(source_ref, ''), COALESCE(tags::text, '[]'),
		       created_at, updated_at
		FROM contracts
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("load contracts: %w", err)
	}
	defer rows.Close()

	contracts := make(map[string]contract)
	for rows.Next() {
		var item contract
		var sourceRef string
		var tagsRaw string
		if err := rows.Scan(&item.ID, &item.Name, &item.SourceType, &sourceRef, &tagsRaw, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan contract: %w", err)
		}
		if sourceRef != "" {
			item.SourceRef = sourceRef
		}
		if err := json.Unmarshal([]byte(tagsRaw), &item.Tags); err != nil {
			return nil, fmt.Errorf("decode contract tags: %w", err)
		}
		contracts[item.ID] = item
	}
	return contracts, rows.Err()
}

func (a *API) loadDocuments(ctx context.Context, conn *pgxpool.Pool, contracts map[string]contract) (map[string]document, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, COALESCE(contract_id::text, ''), source_type, COALESCE(source_ref, ''), COALESCE(tags::text, '[]'),
		       filename, mime_type, status, COALESCE(checksum, ''), COALESCE(extracted_text, ''),
		       COALESCE(storage_key, ''), storage_uri, created_at, updated_at, file_order
		FROM documents
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("load documents: %w", err)
	}
	defer rows.Close()

	type contractFile struct {
		documentID string
		order      int
		createdAt  time.Time
	}

	documents := make(map[string]document)
	contractFiles := make(map[string][]contractFile)
	for rows.Next() {
		var doc document
		var contractID string
		var sourceRef string
		var tagsRaw string
		var fileOrder int
		if err := rows.Scan(
			&doc.ID,
			&contractID,
			&doc.SourceType,
			&sourceRef,
			&tagsRaw,
			&doc.Filename,
			&doc.MIMEType,
			&doc.Status,
			&doc.Checksum,
			&doc.ExtractedText,
			&doc.StorageKey,
			&doc.StorageURI,
			&doc.CreatedAt,
			&doc.UpdatedAt,
			&fileOrder,
		); err != nil {
			return nil, fmt.Errorf("scan document: %w", err)
		}
		doc.ContractID = contractID
		doc.SourceRef = sourceRef
		if err := json.Unmarshal([]byte(tagsRaw), &doc.Tags); err != nil {
			return nil, fmt.Errorf("decode document tags: %w", err)
		}
		documents[doc.ID] = doc
		if contractID != "" {
			contractFiles[contractID] = append(contractFiles[contractID], contractFile{
				documentID: doc.ID,
				order:      fileOrder,
				createdAt:  doc.CreatedAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	for contractID, files := range contractFiles {
		item, ok := contracts[contractID]
		if !ok {
			continue
		}
		sort.Slice(files, func(i, j int) bool {
			if files[i].order == files[j].order {
				return files[i].createdAt.Before(files[j].createdAt)
			}
			return files[i].order < files[j].order
		})
		item.FileIDs = make([]string, 0, len(files))
		for _, file := range files {
			item.FileIDs = append(item.FileIDs, file.documentID)
		}
		contracts[contractID] = item
	}

	return documents, nil
}

func (a *API) loadChecks(ctx context.Context, conn *pgxpool.Pool) (map[string]checkRun, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, status, check_type, requested_at, finished_at, COALESCE(failure_reason, '')
		FROM check_runs
		ORDER BY requested_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("load checks: %w", err)
	}
	defer rows.Close()

	checks := make(map[string]checkRun)
	for rows.Next() {
		var run checkRun
		var finishedAt *time.Time
		if err := rows.Scan(&run.CheckID, &run.Status, &run.CheckType, &run.RequestedAt, &finishedAt, &run.FailureReason); err != nil {
			return nil, fmt.Errorf("scan check: %w", err)
		}
		if finishedAt != nil {
			run.FinishedAt = finishedAt
		}
		run.DocumentIDs, err = a.loadCheckDocumentIDs(ctx, conn, run.CheckID)
		if err != nil {
			return nil, err
		}
		run.Items, err = a.loadCheckItems(ctx, conn, run.CheckID)
		if err != nil {
			return nil, err
		}
		checks[run.CheckID] = run
	}
	return checks, rows.Err()
}

func (a *API) loadCheckDocumentIDs(ctx context.Context, conn *pgxpool.Pool, checkID string) ([]string, error) {
	rows, err := conn.Query(ctx, `
		SELECT document_id
		FROM check_run_documents
		WHERE check_run_id = $1
		ORDER BY position ASC`, checkID)
	if err != nil {
		return nil, fmt.Errorf("load check documents: %w", err)
	}
	defer rows.Close()

	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan check document id: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (a *API) loadCheckItems(ctx context.Context, conn *pgxpool.Pool, checkID string) ([]checkResultItem, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, document_id, outcome, confidence, COALESCE(summary, '')
		FROM check_results
		WHERE check_run_id = $1
		ORDER BY created_at ASC`, checkID)
	if err != nil {
		return nil, fmt.Errorf("load check results: %w", err)
	}
	defer rows.Close()

	items := make([]checkResultItem, 0)
	for rows.Next() {
		var resultID string
		var item checkResultItem
		if err := rows.Scan(&resultID, &item.DocumentID, &item.Outcome, &item.Confidence, &item.Summary); err != nil {
			return nil, fmt.Errorf("scan check result: %w", err)
		}
		evidence, err := a.loadEvidence(ctx, conn, resultID)
		if err != nil {
			return nil, err
		}
		item.Evidence = evidence
		items = append(items, item)
	}
	return items, rows.Err()
}

func (a *API) loadEvidence(ctx context.Context, conn *pgxpool.Pool, resultID string) ([]evidenceSnippet, error) {
	rows, err := conn.Query(ctx, `
		SELECT snippet_text, COALESCE(page_number, 1), COALESCE(chunk_ref, ''), COALESCE(score, 0)
		FROM evidence_snippets
		WHERE check_result_id = $1
		ORDER BY created_at ASC`, resultID)
	if err != nil {
		return nil, fmt.Errorf("load evidence: %w", err)
	}
	defer rows.Close()

	evidence := make([]evidenceSnippet, 0)
	for rows.Next() {
		var snippet evidenceSnippet
		if err := rows.Scan(&snippet.SnippetText, &snippet.PageNumber, &snippet.ChunkID, &snippet.Score); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		evidence = append(evidence, snippet)
	}
	return evidence, rows.Err()
}

func (a *API) loadIdempotency(ctx context.Context, conn *pgxpool.Pool) (map[string]idempotencyRecord, error) {
	rows, err := conn.Query(ctx, `
		SELECT idempotency_key, payload_hash, check_run_id
		FROM idempotency_keys`)
	if err != nil {
		return nil, fmt.Errorf("load idempotency keys: %w", err)
	}
	defer rows.Close()

	keys := make(map[string]idempotencyRecord)
	for rows.Next() {
		var key string
		var rec idempotencyRecord
		if err := rows.Scan(&key, &rec.PayloadHash, &rec.CheckID); err != nil {
			return nil, fmt.Errorf("scan idempotency key: %w", err)
		}
		keys[key] = rec
	}
	return keys, rows.Err()
}

func (a *API) loadCopyEvents(ctx context.Context, conn *pgxpool.Pool) (map[string]externalCopyEvent, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, document_id, target_system, status, COALESCE(request_payload::text, '{}'),
		       COALESCE(response_payload::text, '{}'), attempts, COALESCE(error_message, ''),
		       created_at, updated_at
		FROM external_copy_events
		ORDER BY created_at ASC`)
	if err != nil {
		return nil, fmt.Errorf("load copy events: %w", err)
	}
	defer rows.Close()

	events := make(map[string]externalCopyEvent)
	for rows.Next() {
		var event externalCopyEvent
		var requestRaw string
		var responseRaw string
		if err := rows.Scan(
			&event.ID,
			&event.DocumentID,
			&event.TargetSystem,
			&event.Status,
			&requestRaw,
			&responseRaw,
			&event.Attempts,
			&event.ErrorMessage,
			&event.CreatedAt,
			&event.UpdatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan copy event: %w", err)
		}
		if err := json.Unmarshal([]byte(requestRaw), &event.RequestPayload); err != nil {
			return nil, fmt.Errorf("decode copy request payload: %w", err)
		}
		if err := json.Unmarshal([]byte(responseRaw), &event.ResponseBody); err != nil {
			return nil, fmt.Errorf("decode copy response payload: %w", err)
		}
		events[event.ID] = event
	}
	return events, rows.Err()
}

func (a *API) persistContract(ctx context.Context, item contract) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	tagsRaw, err := json.Marshal(item.Tags)
	if err != nil {
		return fmt.Errorf("encode contract tags: %w", err)
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO contracts (id, name, source_type, source_ref, tags, created_at, updated_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), $5::jsonb, $6, $7)
		ON CONFLICT (id) DO UPDATE
		SET name = EXCLUDED.name,
		    source_type = EXCLUDED.source_type,
		    source_ref = EXCLUDED.source_ref,
		    tags = EXCLUDED.tags,
		    updated_at = EXCLUDED.updated_at`,
		item.ID, item.Name, item.SourceType, item.SourceRef, string(tagsRaw), item.CreatedAt, item.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("persist contract: %w", err)
	}
	return nil
}

func (a *API) persistDocument(ctx context.Context, doc document) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	tagsRaw, err := json.Marshal(doc.Tags)
	if err != nil {
		return fmt.Errorf("encode document tags: %w", err)
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO documents (
			id, contract_id, source_type, source_ref, tags, filename, mime_type, storage_uri,
			status, created_at, updated_at, checksum, extracted_text, storage_key, file_order
		)
		VALUES (
			$1, NULLIF($2, '')::uuid, $3, NULLIF($4, ''), $5::jsonb, $6, $7, $8,
			$9, $10, $11, $12, $13, $14, $15
		)
		ON CONFLICT (id) DO UPDATE
		SET contract_id = EXCLUDED.contract_id,
		    source_type = EXCLUDED.source_type,
		    source_ref = EXCLUDED.source_ref,
		    tags = EXCLUDED.tags,
		    filename = EXCLUDED.filename,
		    mime_type = EXCLUDED.mime_type,
		    storage_uri = EXCLUDED.storage_uri,
		    status = EXCLUDED.status,
		    updated_at = EXCLUDED.updated_at,
		    checksum = EXCLUDED.checksum,
		    extracted_text = EXCLUDED.extracted_text,
		    storage_key = EXCLUDED.storage_key,
		    file_order = EXCLUDED.file_order`,
		doc.ID, doc.ContractID, doc.SourceType, doc.SourceRef, string(tagsRaw), doc.Filename, doc.MIMEType, doc.StorageURI,
		doc.Status, doc.CreatedAt, doc.UpdatedAt, doc.Checksum, doc.ExtractedText, doc.StorageKey, a.fileOrder(doc),
	)
	if err != nil {
		return fmt.Errorf("persist document: %w", err)
	}
	return nil
}

func (a *API) persistCheckCreated(ctx context.Context, run checkRun, payload any, idempotencyKey, payloadHash string) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	payloadRaw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("encode check payload: %w", err)
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin create check tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	if _, err := tx.Exec(ctx, `
		INSERT INTO check_runs (id, check_type, input_payload, status, requested_at, finished_at, failure_reason, created_at)
		VALUES ($1, $2, $3::jsonb, $4, $5, NULL, '', $5)
		ON CONFLICT (id) DO UPDATE
		SET check_type = EXCLUDED.check_type,
		    input_payload = EXCLUDED.input_payload,
		    status = EXCLUDED.status,
		    requested_at = EXCLUDED.requested_at,
		    finished_at = EXCLUDED.finished_at,
		    failure_reason = EXCLUDED.failure_reason`,
		run.CheckID, run.CheckType, string(payloadRaw), run.Status, run.RequestedAt,
	); err != nil {
		return fmt.Errorf("insert check run: %w", err)
	}
	for idx, documentID := range run.DocumentIDs {
		if _, err := tx.Exec(ctx, `
			INSERT INTO check_run_documents (check_run_id, document_id, position)
			VALUES ($1, $2, $3)
			ON CONFLICT (check_run_id, document_id) DO UPDATE
			SET position = EXCLUDED.position`,
			run.CheckID, documentID, idx,
		); err != nil {
			return fmt.Errorf("insert check document: %w", err)
		}
	}
	if idempotencyKey != "" {
		if _, err := tx.Exec(ctx, `
			INSERT INTO idempotency_keys (idempotency_key, check_run_id, payload_hash)
			VALUES ($1, $2, $3)
			ON CONFLICT (idempotency_key) DO UPDATE
			SET check_run_id = EXCLUDED.check_run_id,
			    payload_hash = EXCLUDED.payload_hash`,
			run.CheckType+":"+idempotencyKey, run.CheckID, payloadHash,
		); err != nil {
			return fmt.Errorf("insert idempotency key: %w", err)
		}
	}
	return tx.Commit(ctx)
}

func (a *API) persistCheckState(ctx context.Context, run checkRun) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin check state tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	if _, err := tx.Exec(ctx, `
		UPDATE check_runs
		SET status = $2, requested_at = $3, finished_at = $4, failure_reason = $5
		WHERE id = $1`,
		run.CheckID, run.Status, run.RequestedAt, run.FinishedAt, run.FailureReason,
	); err != nil {
		return fmt.Errorf("update check run: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM check_results WHERE check_run_id = $1`, run.CheckID); err != nil {
		return fmt.Errorf("clear check results: %w", err)
	}
	for _, item := range run.Items {
		var resultID string
		if err := tx.QueryRow(ctx, `
			INSERT INTO check_results (id, check_run_id, document_id, outcome, confidence, summary, created_at)
			VALUES (gen_random_uuid(), $1, $2, $3, $4, NULLIF($5, ''), NOW())
			RETURNING id`,
			run.CheckID, item.DocumentID, item.Outcome, item.Confidence, item.Summary,
		).Scan(&resultID); err != nil {
			return fmt.Errorf("insert check result: %w", err)
		}
		for _, snippet := range item.Evidence {
			if _, err := tx.Exec(ctx, `
				INSERT INTO evidence_snippets (id, check_result_id, page_number, chunk_ref, snippet_text, score, created_at)
				VALUES (gen_random_uuid(), $1, $2, NULLIF($3, ''), $4, $5, NOW())`,
				resultID, snippet.PageNumber, snippet.ChunkID, snippet.SnippetText, snippet.Score,
			); err != nil {
				return fmt.Errorf("insert evidence snippet: %w", err)
			}
		}
	}
	return tx.Commit(ctx)
}

func (a *API) persistCopyEvent(ctx context.Context, event externalCopyEvent) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	requestRaw, err := json.Marshal(event.RequestPayload)
	if err != nil {
		return fmt.Errorf("encode copy request payload: %w", err)
	}
	responseRaw, err := json.Marshal(event.ResponseBody)
	if err != nil {
		return fmt.Errorf("encode copy response payload: %w", err)
	}
	_, err = conn.Exec(ctx, `
		INSERT INTO external_copy_events (
			id, document_id, target_system, request_payload, response_payload, status,
			error_message, created_at, updated_at, attempts
		)
		VALUES ($1, $2, $3, $4::jsonb, $5::jsonb, $6, NULLIF($7, ''), $8, $9, $10)
		ON CONFLICT (id) DO UPDATE
		SET request_payload = EXCLUDED.request_payload,
		    response_payload = EXCLUDED.response_payload,
		    status = EXCLUDED.status,
		    error_message = EXCLUDED.error_message,
		    updated_at = EXCLUDED.updated_at,
		    attempts = EXCLUDED.attempts`,
		event.ID, event.DocumentID, event.TargetSystem, string(requestRaw), string(responseRaw), event.Status,
		event.ErrorMessage, event.CreatedAt, event.UpdatedAt, event.Attempts,
	)
	if err != nil {
		return fmt.Errorf("persist copy event: %w", err)
	}
	return nil
}

func (a *API) deleteDocumentState(ctx context.Context, documentID string) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete document tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM check_runs
		WHERE id IN (
			SELECT DISTINCT check_run_id
			FROM check_run_documents
			WHERE document_id = $1
		)`, documentID); err != nil {
		return fmt.Errorf("delete related checks: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM documents WHERE id = $1`, documentID); err != nil {
		return fmt.Errorf("delete document row: %w", err)
	}
	return tx.Commit(ctx)
}

func (a *API) deleteContractState(ctx context.Context, contractID string) error {
	conn := a.pgPool()
	if conn == nil {
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete contract tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	if _, err := tx.Exec(ctx, `
		DELETE FROM check_runs
		WHERE id IN (
			SELECT DISTINCT crd.check_run_id
			FROM check_run_documents AS crd
			INNER JOIN documents AS d ON d.id = crd.document_id
			WHERE d.contract_id = $1
		)`, contractID); err != nil {
		return fmt.Errorf("delete contract checks: %w", err)
	}
	if _, err := tx.Exec(ctx, `DELETE FROM contracts WHERE id = $1`, contractID); err != nil {
		return fmt.Errorf("delete contract row: %w", err)
	}
	return tx.Commit(ctx)
}

func (a *API) deleteChecksState(ctx context.Context, checkIDs []string) error {
	conn := a.pgPool()
	if conn == nil || len(checkIDs) == 0 {
		return nil
	}
	tx, err := conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin delete checks tx: %w", err)
	}
	defer rollbackTx(ctx, tx)

	params := make([]any, 0, len(checkIDs))
	placeholders := make([]string, 0, len(checkIDs))
	for index, checkID := range checkIDs {
		params = append(params, checkID)
		placeholders = append(placeholders, fmt.Sprintf("$%d", index+1))
	}

	if _, err := tx.Exec(ctx,
		fmt.Sprintf(`DELETE FROM check_runs WHERE id IN (%s)`, strings.Join(placeholders, ", ")),
		params...,
	); err != nil {
		return fmt.Errorf("delete checks: %w", err)
	}
	return tx.Commit(ctx)
}

func (a *API) pgPool() *pgxpool.Pool {
	if a.pg == nil {
		return nil
	}
	return a.pg.Pool()
}

func (a *API) fileOrder(doc document) int {
	if doc.ContractID == "" {
		return 0
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	item, ok := a.contracts[doc.ContractID]
	if !ok {
		return 0
	}
	for idx, fileID := range item.FileIDs {
		if fileID == doc.ID {
			return idx
		}
	}
	return 0
}

func rollbackTx(ctx context.Context, tx pgx.Tx) {
	if tx == nil {
		return
	}
	_ = tx.Rollback(ctx)
}
