package models

import (
	"context"
	"encoding/json"
	"time"

	"legal-doc-intel/go-api/internal/db"
)

type Contracts struct {
	pg *db.Postgres
}

type ContractListRow struct {
	ID         string
	Name       string
	SourceType string
	SourceRef  string
	Tags       []string
	FileCount  int
	CreatedAt  time.Time
	UpdatedAt  time.Time
}

type ContractRow struct {
	ID         string
	Name       string
	SourceType string
	SourceRef  string
	Tags       []string
	CreatedAt  time.Time
	UpdatedAt  time.Time
	Files      []DocumentRow
}

func NewContracts(pg *db.Postgres) *Contracts {
	if pg == nil {
		return nil
	}
	return &Contracts{pg: pg}
}

func (s *Contracts) List(ctx context.Context, limit, offset int) ([]ContractListRow, int, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return nil, 0, db.ErrNotConfigured
	}

	conn := s.pg.Pool()

	var total int
	if err := conn.QueryRow(ctx, `SELECT COUNT(*) FROM contracts`).Scan(&total); err != nil {
		return nil, 0, err
	}

	rows, err := conn.Query(ctx, `
		SELECT c.id,
		       c.name,
		       c.source_type,
		       COALESCE(c.source_ref, ''),
		       COALESCE(c.tags::text, '[]'),
		       c.created_at,
		       c.updated_at,
		       COUNT(d.id) AS file_count
		FROM contracts AS c
		LEFT JOIN documents AS d ON d.contract_id = c.id
		GROUP BY c.id
		ORDER BY c.created_at DESC
		LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()

	items := make([]ContractListRow, 0)
	for rows.Next() {
		var item ContractListRow
		var sourceRef string
		var tagsRaw string
		if err := rows.Scan(
			&item.ID,
			&item.Name,
			&item.SourceType,
			&sourceRef,
			&tagsRaw,
			&item.CreatedAt,
			&item.UpdatedAt,
			&item.FileCount,
		); err != nil {
			return nil, 0, err
		}
		if sourceRef != "" {
			item.SourceRef = sourceRef
		}
		if err := json.Unmarshal([]byte(tagsRaw), &item.Tags); err != nil {
			return nil, 0, err
		}
		items = append(items, item)
	}
	if rows.Err() != nil {
		return nil, 0, rows.Err()
	}

	return items, total, nil
}

func (s *Contracts) Get(ctx context.Context, contractID string) (ContractRow, bool, error) {
	if s == nil || s.pg == nil || s.pg.Pool() == nil {
		return ContractRow{}, false, db.ErrNotConfigured
	}

	conn := s.pg.Pool()

	var item ContractRow
	var sourceRef string
	var tagsRaw string
	err := conn.QueryRow(ctx, `
		SELECT id, name, source_type, COALESCE(source_ref, ''), COALESCE(tags::text, '[]'), created_at, updated_at
		FROM contracts
		WHERE id = $1`, contractID).
		Scan(&item.ID, &item.Name, &item.SourceType, &sourceRef, &tagsRaw, &item.CreatedAt, &item.UpdatedAt)
	if err != nil {
		if isNotFound(err) {
			return ContractRow{}, false, nil
		}
		return ContractRow{}, false, err
	}
	if sourceRef != "" {
		item.SourceRef = sourceRef
	}
	if err := json.Unmarshal([]byte(tagsRaw), &item.Tags); err != nil {
		return ContractRow{}, false, err
	}

	files, err := loadContractDocuments(ctx, conn, contractID)
	if err != nil {
		return ContractRow{}, false, err
	}
	item.Files = files
	return item, true, nil
}
