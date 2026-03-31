package handlers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"time"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/http/middleware"
	"legal-doc-intel/go-api/internal/ids"
)

func (a *API) CreateClauseCheck(w http.ResponseWriter, r *http.Request) {
	var req clauseCheckRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(strings.TrimSpace(req.RequiredClauseText)) < 5 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "required_clause_text must be at least 5 characters", false, nil)
		return
	}

	checkID, status, reused, err := a.createCheck(r, checkTypeClause, req, req.DocumentIDs)
	if err != nil {
		handleCreateCheckError(w, err)
		return
	}
	if reused {
		a.logger.Info("idempotent check request reused", "check_id", checkID, "check_type", checkTypeClause)
		writeJSON(w, http.StatusAccepted, checkAcceptedResponse{CheckID: checkID, Status: status, CheckType: checkTypeClause})
		return
	}

	go a.runClauseCheck(checkID, req, middleware.GetRequestID(r.Context()))
	writeJSON(w, http.StatusAccepted, checkAcceptedResponse{CheckID: checkID, Status: status, CheckType: checkTypeClause})
}

func (a *API) CreateCompanyNameCheck(w http.ResponseWriter, r *http.Request) {
	var req companyNameCheckRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(strings.TrimSpace(req.OldCompanyName)) < 2 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "old_company_name must be at least 2 characters", false, nil)
		return
	}

	checkID, status, reused, err := a.createCheck(r, checkTypeCompany, req, req.DocumentIDs)
	if err != nil {
		handleCreateCheckError(w, err)
		return
	}
	if reused {
		a.logger.Info("idempotent check request reused", "check_id", checkID, "check_type", checkTypeCompany)
		writeJSON(w, http.StatusAccepted, checkAcceptedResponse{CheckID: checkID, Status: status, CheckType: checkTypeCompany})
		return
	}

	go a.runCompanyNameCheck(checkID, req, middleware.GetRequestID(r.Context()))
	writeJSON(w, http.StatusAccepted, checkAcceptedResponse{CheckID: checkID, Status: status, CheckType: checkTypeCompany})
}

func (a *API) CreateLLMReviewCheck(w http.ResponseWriter, r *http.Request) {
	var req llmReviewCheckRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	if len(strings.TrimSpace(req.Instructions)) < 5 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "instructions must be at least 5 characters", false, nil)
		return
	}

	checkID, status, reused, err := a.createCheck(r, checkTypeLLMReview, req, req.DocumentIDs)
	if err != nil {
		handleCreateCheckError(w, err)
		return
	}
	if reused {
		a.logger.Info("idempotent check request reused", "check_id", checkID, "check_type", checkTypeLLMReview)
		writeJSON(w, http.StatusAccepted, checkAcceptedResponse{CheckID: checkID, Status: status, CheckType: checkTypeLLMReview})
		return
	}

	go a.runLLMReviewCheck(checkID, req, middleware.GetRequestID(r.Context()))
	writeJSON(w, http.StatusAccepted, checkAcceptedResponse{CheckID: checkID, Status: status, CheckType: checkTypeLLMReview})
}

func (a *API) GetCheck(w http.ResponseWriter, r *http.Request) {
	checkID := pathParam(r, "check_id")
	if !ids.IsUUID(checkID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "check_id must be a valid UUID", false, nil)
		return
	}

	a.mu.RLock()
	check, ok := a.checks[checkID]
	a.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "check not found", false, nil)
		return
	}

	resp := checkRunResponse{
		CheckID:     check.CheckID,
		Status:      check.Status,
		CheckType:   check.CheckType,
		RequestedAt: check.RequestedAt.Format(time.RFC3339),
	}
	if check.FinishedAt != nil {
		v := check.FinishedAt.Format(time.RFC3339)
		resp.FinishedAt = &v
	}
	if check.FailureReason != "" {
		resp.FailureReason = check.FailureReason
	}

	writeJSON(w, http.StatusOK, resp)
}

func (a *API) GetCheckResults(w http.ResponseWriter, r *http.Request) {
	checkID := pathParam(r, "check_id")
	if !ids.IsUUID(checkID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "check_id must be a valid UUID", false, nil)
		return
	}

	a.mu.RLock()
	check, ok := a.checks[checkID]
	a.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "check not found", false, nil)
		return
	}
	if check.Status != checkStatusCompleted {
		writeError(w, http.StatusConflict, "results_not_ready", "results are not available for this check status", false, map[string]any{"status": check.Status})
		return
	}

	writeJSON(w, http.StatusOK, checkResultsResponse{CheckID: check.CheckID, Status: check.Status, Items: check.Items})
}

func (a *API) DeleteCheck(w http.ResponseWriter, r *http.Request) {
	checkID := pathParam(r, "check_id")
	if !ids.IsUUID(checkID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "check_id must be a valid UUID", false, nil)
		return
	}

	deleted, err := a.deleteChecksByID(r.Context(), []string{checkID})
	if err != nil {
		a.logger.Error("delete check failed", "check_id", checkID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete check", true, nil)
		return
	}
	if deleted == 0 {
		writeError(w, http.StatusNotFound, "not_found", "check not found", false, nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) DeleteChecks(w http.ResponseWriter, r *http.Request) {
	var req deleteChecksRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.CheckIDs) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "check_ids must contain at least one id", false, nil)
		return
	}

	seen := make(map[string]struct{}, len(req.CheckIDs))
	checkIDs := make([]string, 0, len(req.CheckIDs))
	for _, checkID := range req.CheckIDs {
		if !ids.IsUUID(checkID) {
			writeError(w, http.StatusBadRequest, "invalid_argument", "each check_id must be a valid UUID", false, nil)
			return
		}
		if _, ok := seen[checkID]; ok {
			continue
		}
		seen[checkID] = struct{}{}
		checkIDs = append(checkIDs, checkID)
	}

	if _, err := a.deleteChecksByID(r.Context(), checkIDs); err != nil {
		a.logger.Error("bulk delete checks failed", "check_count", len(checkIDs), "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete checks", true, nil)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (a *API) SearchContracts(w http.ResponseWriter, r *http.Request) {
	var req contractSearchRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	queryText := strings.TrimSpace(req.QueryText)
	if len(queryText) < 2 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "query_text must be at least 2 characters", false, nil)
		return
	}
	strategy := strings.TrimSpace(strings.ToLower(req.Strategy))
	if strategy == "" {
		strategy = "semantic"
	}
	if strategy != "semantic" && strategy != "strict" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "strategy must be one of: semantic, strict", false, nil)
		return
	}
	resultMode := strings.TrimSpace(strings.ToLower(req.ResultMode))
	if resultMode == "" {
		resultMode = "sections"
	}
	if resultMode != "sections" && resultMode != "contracts" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "result_mode must be one of: sections, contracts", false, nil)
		return
	}

	limit := req.Limit
	if limit == 0 {
		limit = 10
	}
	if limit < 0 || limit > 50 {
		writeError(w, http.StatusBadRequest, "invalid_argument", "limit must be between 1 and 50", false, nil)
		return
	}

	var resolvedDocIDs []string
	var err error
	if len(req.DocumentIDs) > 0 {
		resolvedDocIDs, err = a.resolveDocumentIDs(req.DocumentIDs)
		if err != nil {
			handleCreateCheckError(w, err)
			return
		}
	} else {
		if a.documentsModel == nil {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		resolvedDocIDs, err = a.documentsModel.ListIDs(r.Context())
		if err != nil {
			if errors.Is(err, db.ErrNotConfigured) {
				writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
				return
			}
			a.logger.Error("list document ids failed", "error", err)
			writeError(w, http.StatusInternalServerError, "internal_error", "failed to load documents", true, nil)
			return
		}
	}

	if len(resolvedDocIDs) == 0 {
		writeJSON(w, http.StatusOK, contractSearchResponse{Items: []contractSearchResultItem{}})
		return
	}

	result, err := a.ai.SearchSections(r.Context(), ai.SearchSectionsRequest{
		JobID:       ids.NewUUID(),
		RequestID:   middleware.GetRequestID(r.Context()),
		QueryText:   queryText,
		DocumentIDs: resolvedDocIDs,
		Limit:       searchCandidateLimit(limit, resultMode),
		Strategy:    strategy,
		ResultMode:  resultMode,
	})
	if err != nil {
		a.logger.Error("contract search failed", "error", err)
		writeError(w, http.StatusBadGateway, "search_unavailable", "search is temporarily unavailable", true, nil)
		return
	}

	docIDs := make([]string, 0, len(result.Items))
	seenDocIDs := make(map[string]struct{}, len(result.Items))
	for _, item := range result.Items {
		if _, ok := seenDocIDs[item.DocumentID]; ok {
			continue
		}
		seenDocIDs[item.DocumentID] = struct{}{}
		docIDs = append(docIDs, item.DocumentID)
	}
	documentsByID, err := a.documentsModel.GetByIDs(r.Context(), docIDs)
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("load documents for search results failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load documents", true, nil)
		return
	}

	items := make([]contractSearchResultItem, 0, len(result.Items))
	for _, item := range result.Items {
		doc, ok := documentsByID[item.DocumentID]
		if !ok {
			continue
		}
		items = append(items, contractSearchResultItem{
			DocumentID:  item.DocumentID,
			ContractID:  doc.ContractID,
			Filename:    doc.Filename,
			PageNumber:  item.PageNumber,
			ChunkID:     item.ChunkID,
			Score:       item.Score,
			SnippetText: item.SnippetText,
		})
	}
	if resultMode == "contracts" {
		items = collapseContractSearchResults(items, limit)
	}
	if len(items) > limit {
		items = items[:limit]
	}

	writeJSON(w, http.StatusOK, contractSearchResponse{Items: items})
}

func searchCandidateLimit(limit int, resultMode string) int {
	if resultMode != "contracts" {
		return limit
	}
	return min(50, max(limit, limit*5))
}

func collapseContractSearchResults(items []contractSearchResultItem, limit int) []contractSearchResultItem {
	bestByGroup := make(map[string]contractSearchResultItem, len(items))
	order := make([]string, 0, len(items))
	for _, item := range items {
		groupKey := strings.TrimSpace(item.ContractID)
		if groupKey == "" {
			groupKey = item.DocumentID
		}

		current, ok := bestByGroup[groupKey]
		if !ok {
			bestByGroup[groupKey] = item
			order = append(order, groupKey)
			continue
		}
		if item.Score > current.Score || (item.Score == current.Score && item.PageNumber < current.PageNumber) {
			bestByGroup[groupKey] = item
		}
	}

	slices.SortStableFunc(order, func(left, right string) int {
		a := bestByGroup[left]
		b := bestByGroup[right]
		switch {
		case a.Score > b.Score:
			return -1
		case a.Score < b.Score:
			return 1
		case a.ContractID < b.ContractID:
			return -1
		case a.ContractID > b.ContractID:
			return 1
		case a.DocumentID < b.DocumentID:
			return -1
		case a.DocumentID > b.DocumentID:
			return 1
		case a.PageNumber < b.PageNumber:
			return -1
		case a.PageNumber > b.PageNumber:
			return 1
		default:
			return 0
		}
	})

	collapsed := make([]contractSearchResultItem, 0, min(limit, len(order)))
	for _, key := range order {
		collapsed = append(collapsed, bestByGroup[key])
		if len(collapsed) >= limit {
			break
		}
	}
	return collapsed
}

func (a *API) createCheck(r *http.Request, checkType string, payload any, documentIDs []string) (checkID string, status string, reused bool, err error) {
	idempotencyKey := strings.TrimSpace(r.Header.Get("Idempotency-Key"))
	if idempotencyKey != "" && (len(idempotencyKey) < 8 || len(idempotencyKey) > 128) {
		return "", "", false, fmt.Errorf("invalid idempotency key")
	}

	resolvedDocIDs, err := a.resolveDocumentIDs(documentIDs)
	if err != nil {
		return "", "", false, err
	}

	payloadHash, err := hashPayload(payload, resolvedDocIDs)
	if err != nil {
		return "", "", false, fmt.Errorf("hash payload: %w", err)
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	if idempotencyKey != "" {
		idempotencyLookupKey := checkType + ":" + idempotencyKey
		if rec, exists := a.idempotency[idempotencyLookupKey]; exists {
			if rec.PayloadHash != payloadHash {
				return "", "", false, errIdempotencyConflict
			}
			run := a.checks[rec.CheckID]
			return run.CheckID, run.Status, true, nil
		}
	}

	checkID = ids.NewUUID()
	now := time.Now().UTC()
	status = checkStatusQueued
	a.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      status,
		CheckType:   checkType,
		RequestedAt: now,
		DocumentIDs: resolvedDocIDs,
	}

	if idempotencyKey != "" {
		a.idempotency[checkType+":"+idempotencyKey] = idempotencyRecord{PayloadHash: payloadHash, CheckID: checkID}
	}
	if err := a.persistCheckCreated(r.Context(), a.checks[checkID], payload, idempotencyKey, payloadHash); err != nil {
		delete(a.checks, checkID)
		if idempotencyKey != "" {
			delete(a.idempotency, checkType+":"+idempotencyKey)
		}
		return "", "", false, fmt.Errorf("persist check: %w", err)
	}
	a.emitAuditEvent("check.created", "check", checkID, map[string]any{
		"check_type":     checkType,
		"document_count": len(resolvedDocIDs),
	})

	return checkID, status, false, nil
}

func (a *API) deleteChecksByID(ctx context.Context, checkIDs []string) (int, error) {
	if len(checkIDs) == 0 {
		return 0, nil
	}

	checksToDelete := make([]checkRun, 0, len(checkIDs))

	a.mu.Lock()
	for _, checkID := range checkIDs {
		run, ok := a.checks[checkID]
		if !ok {
			continue
		}
		checksToDelete = append(checksToDelete, run)
	}

	if err := a.deleteChecksState(ctx, checkIDs); err != nil {
		a.mu.Unlock()
		return 0, err
	}

	for _, run := range checksToDelete {
		delete(a.checks, run.CheckID)
		for key, rec := range a.idempotency {
			if rec.CheckID == run.CheckID {
				delete(a.idempotency, key)
			}
		}
		a.emitAuditEvent("check.deleted", "check", run.CheckID, map[string]any{
			"check_type": run.CheckType,
		})
	}
	a.mu.Unlock()

	return len(checksToDelete), nil
}

func (a *API) resolveDocumentIDs(explicit []string) ([]string, error) {
	if a.documentsModel == nil {
		return nil, db.ErrNotConfigured
	}
	if len(explicit) == 0 {
		documentIDs, err := a.documentsModel.ListIDs(context.Background())
		if err != nil {
			return nil, err
		}
		if len(documentIDs) == 0 {
			return nil, fmt.Errorf("at least one document is required")
		}
		return documentIDs, nil
	}

	if len(explicit) == 0 {
		return nil, fmt.Errorf("at least one document is required")
	}

	for _, id := range explicit {
		if !ids.IsUUID(id) {
			return nil, fmt.Errorf("document_id must be a valid UUID: %s", id)
		}
	}
	return a.documentsModel.ResolveIDs(context.Background(), explicit)
}

func (a *API) runClauseCheck(checkID string, req clauseCheckRequest, requestID string) {
	if !a.markCheckRunning(checkID) {
		return
	}

	a.mu.RLock()
	run := a.checks[checkID]
	a.mu.RUnlock()

	result, err := a.ai.AnalyzeClause(context.Background(), ai.AnalyzeClauseRequest{
		JobID:              ids.NewUUID(),
		RequestID:          requestID,
		CheckID:            checkID,
		DocumentIDs:        run.DocumentIDs,
		RequiredClauseText: req.RequiredClauseText,
		ContextHint:        req.ContextHint,
	})
	if err != nil {
		a.markCheckFailed(checkID, err)
		return
	}

	items := mapAnalysisItems(run.DocumentIDs, result.Items, "Clause analysis returned no items; manual review is required.")
	a.markCheckCompleted(checkID, items)
}

func (a *API) runCompanyNameCheck(checkID string, req companyNameCheckRequest, requestID string) {
	if !a.markCheckRunning(checkID) {
		return
	}

	a.mu.RLock()
	run := a.checks[checkID]
	a.mu.RUnlock()

	result, err := a.ai.AnalyzeCompanyName(context.Background(), ai.AnalyzeCompanyNameRequest{
		JobID:          ids.NewUUID(),
		RequestID:      requestID,
		CheckID:        checkID,
		DocumentIDs:    run.DocumentIDs,
		OldCompanyName: req.OldCompanyName,
		NewCompanyName: req.NewCompanyName,
	})
	if err != nil {
		a.markCheckFailed(checkID, err)
		return
	}

	items := mapAnalysisItems(run.DocumentIDs, result.Items, "Company-name analysis returned no items; manual review is required.")
	a.markCheckCompleted(checkID, items)
}

func (a *API) runLLMReviewCheck(checkID string, req llmReviewCheckRequest, requestID string) {
	if !a.markCheckRunning(checkID) {
		return
	}

	a.mu.RLock()
	run := a.checks[checkID]
	a.mu.RUnlock()

	docMap, err := a.documentsModel.GetByIDs(context.Background(), run.DocumentIDs)
	if err != nil {
		a.markCheckFailed(checkID, err)
		return
	}

	documents := make([]ai.AnalyzeDocument, 0, len(run.DocumentIDs))
	for _, documentID := range run.DocumentIDs {
		doc, ok := docMap[documentID]
		if !ok {
			continue
		}
		documents = append(documents, ai.AnalyzeDocument{
			DocumentID: documentID,
			Filename:   doc.Filename,
			Text:       doc.ExtractedText,
		})
	}

	result, err := a.ai.AnalyzeLLMReview(context.Background(), ai.AnalyzeLLMReviewRequest{
		JobID:        ids.NewUUID(),
		RequestID:    requestID,
		CheckID:      checkID,
		DocumentIDs:  run.DocumentIDs,
		Instructions: req.Instructions,
		Documents:    documents,
	})
	if err != nil {
		a.markCheckFailed(checkID, err)
		return
	}

	items := mapAnalysisItems(run.DocumentIDs, result.Items, "LLM review returned no items; manual review is required.")
	a.markCheckCompleted(checkID, items)
}

func mapAnalysisItems(documentIDs []string, analysisItems []ai.AnalysisResultItem, fallbackSummary string) []checkResultItem {
	byDocument := make(map[string]ai.AnalysisResultItem, len(analysisItems))
	for _, item := range analysisItems {
		if item.DocumentID == "" {
			continue
		}
		byDocument[item.DocumentID] = item
	}

	items := make([]checkResultItem, 0, len(documentIDs))
	for _, documentID := range documentIDs {
		analysisItem, ok := byDocument[documentID]
		if !ok {
			items = append(items, checkResultItem{
				DocumentID: documentID,
				Outcome:    "review",
				Confidence: 0.35,
				Summary:    fallbackSummary,
			})
			continue
		}

		evidence := make([]evidenceSnippet, 0, len(analysisItem.Evidence))
		for _, snippet := range analysisItem.Evidence {
			evidence = append(evidence, evidenceSnippet{
				SnippetText: snippet.SnippetText,
				PageNumber:  snippet.PageNumber,
				ChunkID:     snippet.ChunkID,
				Score:       snippet.Score,
			})
		}

		items = append(items, checkResultItem{
			DocumentID: documentID,
			Outcome:    analysisItem.Outcome,
			Confidence: analysisItem.Confidence,
			Summary:    analysisItem.Summary,
			Evidence:   evidence,
		})
	}

	return items
}

func (a *API) markCheckRunning(checkID string) bool {
	a.mu.Lock()
	run, ok := a.checks[checkID]
	if !ok {
		a.mu.Unlock()
		return false
	}
	run.Status = checkStatusRunning
	a.checks[checkID] = run
	a.mu.Unlock()
	_ = a.persistCheckState(context.Background(), run)
	return true
}

func (a *API) markCheckCompleted(checkID string, items []checkResultItem) {
	now := time.Now().UTC()

	a.mu.Lock()
	run := a.checks[checkID]
	run.Status = checkStatusCompleted
	run.FinishedAt = &now
	run.Items = items
	a.checks[checkID] = run
	a.mu.Unlock()
	_ = a.persistCheckState(context.Background(), run)
	a.emitAuditEvent("check.completed", "check", checkID, map[string]any{"item_count": len(items)})
}

func (a *API) markCheckFailed(checkID string, err error) {
	now := time.Now().UTC()

	a.mu.Lock()
	run := a.checks[checkID]
	run.Status = checkStatusFailed
	run.FinishedAt = &now
	run.FailureReason = err.Error()
	a.checks[checkID] = run
	a.mu.Unlock()
	_ = a.persistCheckState(context.Background(), run)
	a.logger.Error("check execution failed", "check_id", checkID, "error", err)
	a.emitAuditEvent("check.failed", "check", checkID, map[string]any{"error": err.Error()})
}

func (a *API) emitAuditEvent(eventType, entityType, entityID string, payload map[string]any) {
	a.logger.Info("audit event", "event_type", eventType, "entity_type", entityType, "entity_id", entityID, "payload", payload)
}

func handleCreateCheckError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, db.ErrNotConfigured):
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
	case errors.Is(err, errIdempotencyConflict):
		writeError(w, http.StatusConflict, "idempotency_conflict", "Idempotency-Key is already used with a different payload", false, nil)
	case strings.Contains(err.Error(), "document not found"):
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
	case strings.Contains(err.Error(), "document_id must be"):
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
	case strings.Contains(err.Error(), "at least one document"):
		writeError(w, http.StatusUnprocessableEntity, "invalid_scope", err.Error(), false, nil)
	case strings.Contains(err.Error(), "idempotency"):
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
	default:
		writeError(w, http.StatusInternalServerError, "internal_error", "could not create check", true, nil)
	}
}
