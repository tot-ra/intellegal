package models

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"legal-doc-intel/go-api/internal/db"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Documents struct {
	pg *db.Postgres
}

type DocumentRow struct {
	ID            string
	ContractID    string
	SourceType    string
	SourceRef     string
	Tags          []string
	Filename      string
	MIMEType      string
	Status        string
	Checksum      string
	ExtractedText string
	StorageKey    string
	StorageURI    string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

type DocumentsListFilter struct {
	Status     string
	SourceType string
	Tags       []string
	Limit      int
	Offset     int
}

func NewDocuments(pg *db.Postgres) *Documents {
	if pg == nil {
		return nil
	}
	return &Documents{pg: pg}
}

func (s *Documents) List(ctx context.Context, filter DocumentsListFilter) ([]DocumentRow, int, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return nil, 0, db.ErrNotConfigured
	}

	items, err := s.loadDocuments(ctx)
	if err != nil {
		return nil, 0, err
	}

	filtered := make([]DocumentRow, 0, len(items))
	for _, item := range items {
		if filter.Status != "" && item.Status != filter.Status {
			continue
		}
		if filter.SourceType != "" && item.SourceType != filter.SourceType {
			continue
		}
		if len(filter.Tags) > 0 && !documentHasAnyTag(item.Tags, filter.Tags) {
			continue
		}
		filtered = append(filtered, item)
	}

	total := len(filtered)
	offset := filter.Offset
	if offset > total {
		offset = total
	}
	end := offset + filter.Limit
	if end > total {
		end = total
	}

	return filtered[offset:end], total, nil
}

func (s *Documents) Get(ctx context.Context, documentID string) (DocumentRow, bool, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return DocumentRow{}, false, db.ErrNotConfigured
	}

	row, found, err := s.getDocument(ctx, documentID)
	if err != nil || !found {
		return row, found, err
	}
	return row, true, nil
}

func (s *Documents) ListIDs(ctx context.Context) ([]string, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return nil, db.ErrNotConfigured
	}

	rows, err := s.pg.Pool().Query(ctx, `SELECT id FROM documents ORDER BY id ASC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	ids := make([]string, 0)
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return ids, nil
}

func (s *Documents) ResolveIDs(ctx context.Context, explicit []string) ([]string, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return nil, db.ErrNotConfigured
	}

	documentIDs := explicit
	if len(documentIDs) == 0 {
		return s.ListIDs(ctx)
	}

	seen := make(map[string]struct{}, len(documentIDs))
	resolved := make([]string, 0, len(documentIDs))
	for _, id := range documentIDs {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		found, err := s.Exists(ctx, id)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, fmt.Errorf("document not found: %s", id)
		}
		resolved = append(resolved, id)
	}
	sort.Strings(resolved)
	return resolved, nil
}

func (s *Documents) Exists(ctx context.Context, documentID string) (bool, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return false, db.ErrNotConfigured
	}

	var found bool
	if err := s.pg.Pool().QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM documents WHERE id = $1)`, documentID).Scan(&found); err != nil {
		return false, err
	}
	return found, nil
}

func (s *Documents) GetByIDs(ctx context.Context, documentIDs []string) (map[string]DocumentRow, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return nil, db.ErrNotConfigured
	}

	items := make(map[string]DocumentRow, len(documentIDs))
	for _, id := range documentIDs {
		item, found, err := s.getDocument(ctx, id)
		if err != nil {
			return nil, err
		}
		if found {
			items[id] = item
		}
	}
	return items, nil
}

func (s *Documents) loadDocuments(ctx context.Context) ([]DocumentRow, error) {
	rows, err := s.pg.Pool().Query(ctx, `
		SELECT id, COALESCE(contract_id::text, ''), source_type, COALESCE(source_ref, ''), COALESCE(tags::text, '[]'),
		       filename, mime_type, status, COALESCE(checksum, ''), COALESCE(extracted_text, ''),
		       COALESCE(storage_key, ''), storage_uri, created_at, updated_at
		FROM documents
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]DocumentRow, 0)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return items, nil
}

func (s *Documents) getDocument(ctx context.Context, documentID string) (DocumentRow, bool, error) {
	row := s.pg.Pool().QueryRow(ctx, `
		SELECT id, COALESCE(contract_id::text, ''), source_type, COALESCE(source_ref, ''), COALESCE(tags::text, '[]'),
		       filename, mime_type, status, COALESCE(checksum, ''), COALESCE(extracted_text, ''),
		       COALESCE(storage_key, ''), storage_uri, created_at, updated_at
		FROM documents
		WHERE id = $1`, documentID)

	item, err := scanDocument(row)
	if err != nil {
		if isNotFound(err) {
			return DocumentRow{}, false, nil
		}
		return DocumentRow{}, false, err
	}
	return item, true, nil
}

func loadContractDocuments(ctx context.Context, conn *pgxpool.Pool, contractID string) ([]DocumentRow, error) {
	rows, err := conn.Query(ctx, `
		SELECT id, COALESCE(contract_id::text, ''), source_type, COALESCE(source_ref, ''), COALESCE(tags::text, '[]'),
		       filename, mime_type, status, COALESCE(checksum, ''), COALESCE(extracted_text, ''),
		       COALESCE(storage_key, ''), storage_uri, created_at, updated_at
		FROM documents
		WHERE contract_id = $1
		ORDER BY file_order ASC, created_at ASC`, contractID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	items := make([]DocumentRow, 0)
	for rows.Next() {
		item, err := scanDocument(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, rows.Err()
	}
	return items, nil
}

type scanner interface {
	Scan(dest ...any) error
}

func scanDocument(row scanner) (DocumentRow, error) {
	var item DocumentRow
	var contractID string
	var sourceRef string
	var tagsRaw string
	if err := row.Scan(
		&item.ID,
		&contractID,
		&item.SourceType,
		&sourceRef,
		&tagsRaw,
		&item.Filename,
		&item.MIMEType,
		&item.Status,
		&item.Checksum,
		&item.ExtractedText,
		&item.StorageKey,
		&item.StorageURI,
		&item.CreatedAt,
		&item.UpdatedAt,
	); err != nil {
		return DocumentRow{}, err
	}
	item.ContractID = contractID
	item.SourceRef = sourceRef
	if err := json.Unmarshal([]byte(tagsRaw), &item.Tags); err != nil {
		return DocumentRow{}, err
	}
	return item, nil
}

func documentHasAnyTag(documentTags, filters []string) bool {
	if len(documentTags) == 0 || len(filters) == 0 {
		return false
	}

	tagSet := make(map[string]struct{}, len(documentTags))
	for _, tag := range documentTags {
		tagSet[strings.ToLower(strings.TrimSpace(tag))] = struct{}{}
	}
	for _, filter := range filters {
		if _, ok := tagSet[strings.ToLower(strings.TrimSpace(filter))]; ok {
			return true
		}
	}
	return false
}
