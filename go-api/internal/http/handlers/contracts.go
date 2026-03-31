package handlers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/http/middleware"
	"legal-doc-intel/go-api/internal/ids"
)

func (a *API) CreateContract(w http.ResponseWriter, r *http.Request) {
	var req createContractRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	name := strings.TrimSpace(req.Name)
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_argument", "name is required", false, nil)
		return
	}

	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = "upload"
	}
	if _, ok := validSourceTypes[sourceType]; !ok {
		writeError(w, http.StatusBadRequest, "invalid_argument", "unsupported source_type", false, nil)
		return
	}

	tags, err := normalizeTags(req.Tags)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
		return
	}

	now := time.Now().UTC()
	item := contract{
		ID:         ids.NewUUID(),
		Name:       name,
		SourceType: sourceType,
		SourceRef:  req.SourceRef,
		Tags:       tags,
		FileIDs:    nil,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	a.mu.Lock()
	a.contracts[item.ID] = item
	a.mu.Unlock()
	if err := a.persistContract(r.Context(), item); err != nil {
		a.mu.Lock()
		delete(a.contracts, item.ID)
		a.mu.Unlock()
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist contract", true, nil)
		return
	}

	a.emitAuditEvent("contract.created", "contract", item.ID, map[string]any{
		"name":        item.Name,
		"source_type": item.SourceType,
	})
	writeJSON(w, http.StatusCreated, mapContract(item, nil))
}

func (a *API) ListContracts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	limit := 50
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > 200 {
			writeError(w, http.StatusBadRequest, "invalid_argument", "limit must be between 1 and 200", false, nil)
			return
		}
		limit = n
	}
	offset := 0
	if v := q.Get("offset"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 0 {
			writeError(w, http.StatusBadRequest, "invalid_argument", "offset must be >= 0", false, nil)
			return
		}
		offset = n
	}

	if a.contractsModel == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	items, total, err := a.contractsModel.List(r.Context(), limit, offset)
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("list contracts query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load contracts", true, nil)
		return
	}
	respItems := make([]contractResponse, 0, len(items))
	for _, item := range items {
		respItems = append(respItems, contractResponse{
			ID:         item.ID,
			Name:       item.Name,
			SourceType: item.SourceType,
			SourceRef:  item.SourceRef,
			Tags:       item.Tags,
			FileCount:  item.FileCount,
			CreatedAt:  item.CreatedAt.Format(time.RFC3339),
			UpdatedAt:  item.UpdatedAt.Format(time.RFC3339),
		})
	}
	writeJSON(w, http.StatusOK, contractListResponse{Items: respItems, Limit: limit, Offset: offset, Total: total})
}

func (a *API) GetContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !ids.IsUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	if a.contractsModel == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	item, ok, err := a.contractsModel.Get(r.Context(), contractID)
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("get contract query failed", "contract_id", contractID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load contract", true, nil)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}

	files := make([]documentResponse, 0, len(item.Files))
	for _, file := range item.Files {
		files = append(files, mapDocument(document{
			ID:            file.ID,
			ContractID:    file.ContractID,
			SourceType:    file.SourceType,
			SourceRef:     file.SourceRef,
			Tags:          file.Tags,
			Filename:      file.Filename,
			MIMEType:      file.MIMEType,
			Status:        file.Status,
			Checksum:      file.Checksum,
			ExtractedText: file.ExtractedText,
			StorageKey:    file.StorageKey,
			StorageURI:    file.StorageURI,
			CreatedAt:     file.CreatedAt,
			UpdatedAt:     file.UpdatedAt,
		}))
	}
	writeJSON(w, http.StatusOK, contractResponse{
		ID:         item.ID,
		Name:       item.Name,
		SourceType: item.SourceType,
		SourceRef:  item.SourceRef,
		Tags:       item.Tags,
		FileCount:  len(item.Files),
		Files:      files,
		CreatedAt:  item.CreatedAt.Format(time.RFC3339),
		UpdatedAt:  item.UpdatedAt.Format(time.RFC3339),
	})
}

func (a *API) UpdateContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !ids.IsUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	var req updateContractRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Name == nil && req.Tags == nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", "at least one of name or tags is required", false, nil)
		return
	}

	a.mu.Lock()
	item, ok := a.contracts[contractID]
	if !ok {
		a.mu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			a.mu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_argument", "name is required", false, nil)
			return
		}
		item.Name = name
	}

	if req.Tags != nil {
		tags, err := normalizeTags(*req.Tags)
		if err != nil {
			a.mu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
			return
		}
		item.Tags = tags
	}

	item.UpdatedAt = time.Now().UTC()
	a.contracts[contractID] = item
	files := make([]documentResponse, 0, len(item.FileIDs))
	for _, fileID := range item.FileIDs {
		if doc, exists := a.documents[fileID]; exists {
			files = append(files, mapDocument(doc))
		}
	}
	a.mu.Unlock()

	if err := a.persistContract(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist contract", true, nil)
		return
	}
	a.emitAuditEvent("contract.updated", "contract", contractID, map[string]any{
		"name": item.Name,
		"tags": item.Tags,
	})

	writeJSON(w, http.StatusOK, mapContract(item, files))
}

func (a *API) DeleteContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !ids.IsUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	a.mu.RLock()
	item, ok := a.contracts[contractID]
	a.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}

	for _, fileID := range item.FileIDs {
		a.mu.RLock()
		doc, exists := a.documents[fileID]
		a.mu.RUnlock()
		if !exists {
			continue
		}
		if err := a.store.Delete(r.Context(), doc.StorageKey); err != nil {
			a.logger.Error("document storage delete failed", "document_id", fileID, "error", err)
			writeError(w, http.StatusBadGateway, "storage_unavailable", "failed to delete document asset", true, nil)
			return
		}
	}

	if err := a.deleteContractState(r.Context(), contractID); err != nil {
		a.logger.Error("contract metadata delete failed", "contract_id", contractID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete contract metadata", true, nil)
		return
	}

	a.mu.Lock()
	delete(a.contracts, contractID)
	deletedChecks := make(map[string]struct{})
	for _, fileID := range item.FileIDs {
		delete(a.documents, fileID)
		for checkID, run := range a.checks {
			if containsString(run.DocumentIDs, fileID) {
				delete(a.checks, checkID)
				deletedChecks[checkID] = struct{}{}
			}
		}
		for eventID, event := range a.copyEvents {
			if event.DocumentID == fileID {
				delete(a.copyEvents, eventID)
			}
		}
	}
	for key, rec := range a.idempotency {
		if _, ok := deletedChecks[rec.CheckID]; ok {
			delete(a.idempotency, key)
		}
	}
	a.mu.Unlock()
	a.emitAuditEvent("contract.deleted", "contract", contractID, map[string]any{"file_count": len(item.FileIDs)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) AddContractFile(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !ids.IsUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	var req createDocumentRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	req.ContractID = contractID
	doc, err := a.createDocumentFromRequest(r.Context(), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		a.writeCreateDocumentError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mapDocument(doc))
}

func (a *API) ReorderContractFiles(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !ids.IsUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	var req reorderContractFilesRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	a.mu.Lock()
	item, ok := a.contracts[contractID]
	if !ok {
		a.mu.Unlock()
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}

	if len(req.FileIDs) != len(item.FileIDs) {
		a.mu.Unlock()
		writeError(w, http.StatusBadRequest, "invalid_argument", "file_ids must contain all contract file ids exactly once", false, nil)
		return
	}

	expected := make(map[string]struct{}, len(item.FileIDs))
	for _, id := range item.FileIDs {
		expected[id] = struct{}{}
	}
	seen := make(map[string]struct{}, len(req.FileIDs))
	for _, id := range req.FileIDs {
		if _, ok := expected[id]; !ok {
			a.mu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_argument", "file_ids contains an unknown file id", false, nil)
			return
		}
		if _, ok := seen[id]; ok {
			a.mu.Unlock()
			writeError(w, http.StatusBadRequest, "invalid_argument", "file_ids must not contain duplicates", false, nil)
			return
		}
		seen[id] = struct{}{}
	}

	item.FileIDs = append([]string{}, req.FileIDs...)
	item.UpdatedAt = time.Now().UTC()
	a.contracts[contractID] = item
	files := make([]documentResponse, 0, len(item.FileIDs))
	for _, fileID := range item.FileIDs {
		if doc, exists := a.documents[fileID]; exists {
			files = append(files, mapDocument(doc))
		}
	}
	a.mu.Unlock()
	if err := a.persistContract(r.Context(), item); err != nil {
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to persist contract", true, nil)
		return
	}
	for _, fileID := range item.FileIDs {
		a.mu.RLock()
		doc, exists := a.documents[fileID]
		a.mu.RUnlock()
		if exists {
			_ = a.persistDocument(context.Background(), doc)
		}
	}
	a.emitAuditEvent("contract.files_reordered", "contract", contractID, map[string]any{"file_count": len(item.FileIDs)})
	writeJSON(w, http.StatusOK, mapContract(item, files))
}
