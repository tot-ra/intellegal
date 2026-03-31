package handlers

import (
	"context"
	"errors"
	"sort"

	"legal-doc-intel/go-api/internal/models"
)

type inMemoryContractReader struct {
	api *API
}

func (r *inMemoryContractReader) List(_ context.Context, limit, offset int) ([]models.ContractListRow, int, error) {
	r.api.mu.RLock()
	items := make([]contract, 0, len(r.api.contracts))
	for _, item := range r.api.contracts {
		items = append(items, item)
	}
	r.api.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	out := make([]models.ContractListRow, 0, end-offset)
	for _, item := range items[offset:end] {
		out = append(out, models.ContractListRow{
			ID:         item.ID,
			Name:       item.Name,
			SourceType: item.SourceType,
			SourceRef:  item.SourceRef,
			Tags:       append([]string(nil), item.Tags...),
			FileCount:  len(item.FileIDs),
			CreatedAt:  item.CreatedAt,
			UpdatedAt:  item.UpdatedAt,
		})
	}
	return out, total, nil
}

func (r *inMemoryContractReader) Get(_ context.Context, contractID string) (models.ContractRow, bool, error) {
	r.api.mu.RLock()
	item, ok := r.api.contracts[contractID]
	if !ok {
		r.api.mu.RUnlock()
		return models.ContractRow{}, false, nil
	}
	files := make([]models.DocumentRow, 0, len(item.FileIDs))
	for _, fileID := range item.FileIDs {
		if doc, exists := r.api.documents[fileID]; exists {
			files = append(files, models.DocumentRow{
				ID:            doc.ID,
				ContractID:    doc.ContractID,
				SourceType:    doc.SourceType,
				SourceRef:     doc.SourceRef,
				Tags:          append([]string(nil), doc.Tags...),
				Filename:      doc.Filename,
				MIMEType:      doc.MIMEType,
				Status:        doc.Status,
				Checksum:      doc.Checksum,
				ExtractedText: doc.ExtractedText,
				StorageKey:    doc.StorageKey,
				StorageURI:    doc.StorageURI,
				CreatedAt:     doc.CreatedAt,
				UpdatedAt:     doc.UpdatedAt,
			})
		}
	}
	r.api.mu.RUnlock()

	return models.ContractRow{
		ID:         item.ID,
		Name:       item.Name,
		SourceType: item.SourceType,
		SourceRef:  item.SourceRef,
		Tags:       append([]string(nil), item.Tags...),
		CreatedAt:  item.CreatedAt,
		UpdatedAt:  item.UpdatedAt,
		Files:      files,
	}, true, nil
}

type inMemoryDocumentReader struct {
	api *API
}

func (r *inMemoryDocumentReader) List(_ context.Context, filter models.DocumentsListFilter) ([]models.DocumentRow, int, error) {
	r.api.mu.RLock()
	items := make([]document, 0, len(r.api.documents))
	for _, doc := range r.api.documents {
		items = append(items, doc)
	}
	r.api.mu.RUnlock()

	filtered := make([]document, 0, len(items))
	for _, doc := range items {
		if filter.Status != "" && doc.Status != filter.Status {
			continue
		}
		if filter.SourceType != "" && doc.SourceType != filter.SourceType {
			continue
		}
		if len(filter.Tags) > 0 && !documentHasAnyTag(doc, filter.Tags) {
			continue
		}
		filtered = append(filtered, doc)
	}
	sort.Slice(filtered, func(i, j int) bool { return filtered[i].CreatedAt.After(filtered[j].CreatedAt) })
	total := len(filtered)
	if filter.Offset > total {
		filter.Offset = total
	}
	end := filter.Offset + filter.Limit
	if end > total {
		end = total
	}

	out := make([]models.DocumentRow, 0, end-filter.Offset)
	for _, doc := range filtered[filter.Offset:end] {
		out = append(out, models.DocumentRow{
			ID:            doc.ID,
			ContractID:    doc.ContractID,
			SourceType:    doc.SourceType,
			SourceRef:     doc.SourceRef,
			Tags:          append([]string(nil), doc.Tags...),
			Filename:      doc.Filename,
			MIMEType:      doc.MIMEType,
			Status:        doc.Status,
			Checksum:      doc.Checksum,
			ExtractedText: doc.ExtractedText,
			StorageKey:    doc.StorageKey,
			StorageURI:    doc.StorageURI,
			CreatedAt:     doc.CreatedAt,
			UpdatedAt:     doc.UpdatedAt,
		})
	}
	return out, total, nil
}

func (r *inMemoryDocumentReader) Get(_ context.Context, documentID string) (models.DocumentRow, bool, error) {
	r.api.mu.RLock()
	doc, ok := r.api.documents[documentID]
	r.api.mu.RUnlock()
	if !ok {
		return models.DocumentRow{}, false, nil
	}
	return models.DocumentRow{
		ID:            doc.ID,
		ContractID:    doc.ContractID,
		SourceType:    doc.SourceType,
		SourceRef:     doc.SourceRef,
		Tags:          append([]string(nil), doc.Tags...),
		Filename:      doc.Filename,
		MIMEType:      doc.MIMEType,
		Status:        doc.Status,
		Checksum:      doc.Checksum,
		ExtractedText: doc.ExtractedText,
		StorageKey:    doc.StorageKey,
		StorageURI:    doc.StorageURI,
		CreatedAt:     doc.CreatedAt,
		UpdatedAt:     doc.UpdatedAt,
	}, true, nil
}

func (r *inMemoryDocumentReader) ListIDs(_ context.Context) ([]string, error) {
	r.api.mu.RLock()
	ids := make([]string, 0, len(r.api.documents))
	for id := range r.api.documents {
		ids = append(ids, id)
	}
	r.api.mu.RUnlock()
	sort.Strings(ids)
	return ids, nil
}

func (r *inMemoryDocumentReader) ResolveIDs(_ context.Context, explicit []string) ([]string, error) {
	seen := make(map[string]struct{}, len(explicit))
	resolved := make([]string, 0, len(explicit))
	r.api.mu.RLock()
	defer r.api.mu.RUnlock()
	for _, id := range explicit {
		if _, ok := r.api.documents[id]; !ok {
			return nil, errors.New("document not found: " + id)
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		resolved = append(resolved, id)
	}
	sort.Strings(resolved)
	return resolved, nil
}

func (r *inMemoryDocumentReader) Exists(_ context.Context, documentID string) (bool, error) {
	r.api.mu.RLock()
	_, ok := r.api.documents[documentID]
	r.api.mu.RUnlock()
	return ok, nil
}

func (r *inMemoryDocumentReader) GetByIDs(_ context.Context, documentIDs []string) (map[string]models.DocumentRow, error) {
	out := make(map[string]models.DocumentRow, len(documentIDs))
	for _, id := range documentIDs {
		doc, ok, _ := r.Get(context.Background(), id)
		if ok {
			out[id] = doc
		}
	}
	return out, nil
}
