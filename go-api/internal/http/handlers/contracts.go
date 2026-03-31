package handlers

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"legal-doc-intel/go-api/internal/http/middleware"
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
		ID:         newUUID(),
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

	a.mu.RLock()
	items := make([]contract, 0, len(a.contracts))
	for _, item := range a.contracts {
		items = append(items, item)
	}
	a.mu.RUnlock()

	sort.Slice(items, func(i, j int) bool { return items[i].CreatedAt.After(items[j].CreatedAt) })
	total := len(items)
	if offset > total {
		offset = total
	}
	end := offset + limit
	if end > total {
		end = total
	}

	respItems := make([]contractResponse, 0, end-offset)
	for _, item := range items[offset:end] {
		respItems = append(respItems, mapContract(item, nil))
	}

	writeJSON(w, http.StatusOK, contractListResponse{Items: respItems, Limit: limit, Offset: offset, Total: total})
}

func (a *API) GetContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !isUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	a.mu.RLock()
	item, ok := a.contracts[contractID]
	if !ok {
		a.mu.RUnlock()
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}
	files := make([]documentResponse, 0, len(item.FileIDs))
	for _, fileID := range item.FileIDs {
		if doc, exists := a.documents[fileID]; exists {
			files = append(files, mapDocument(doc))
		}
	}
	a.mu.RUnlock()

	writeJSON(w, http.StatusOK, mapContract(item, files))
}

func (a *API) UpdateContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !isUUID(contractID) {
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
	defer a.mu.Unlock()

	item, ok := a.contracts[contractID]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			writeError(w, http.StatusBadRequest, "invalid_argument", "name is required", false, nil)
			return
		}
		item.Name = name
	}

	if req.Tags != nil {
		tags, err := normalizeTags(*req.Tags)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
			return
		}
		item.Tags = tags
	}

	item.UpdatedAt = time.Now().UTC()
	a.contracts[contractID] = item
	a.emitAuditEvent("contract.updated", "contract", contractID, map[string]any{
		"name": item.Name,
		"tags": item.Tags,
	})

	files := make([]documentResponse, 0, len(item.FileIDs))
	for _, fileID := range item.FileIDs {
		if doc, exists := a.documents[fileID]; exists {
			files = append(files, mapDocument(doc))
		}
	}
	writeJSON(w, http.StatusOK, mapContract(item, files))
}

func (a *API) DeleteContract(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !isUUID(contractID) {
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

	a.mu.Lock()
	delete(a.contracts, contractID)
	for _, fileID := range item.FileIDs {
		delete(a.documents, fileID)
	}
	a.mu.Unlock()
	a.emitAuditEvent("contract.deleted", "contract", contractID, map[string]any{"file_count": len(item.FileIDs)})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) AddContractFile(w http.ResponseWriter, r *http.Request) {
	contractID := pathParam(r, "contract_id")
	if !isUUID(contractID) {
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
	if !isUUID(contractID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "contract_id must be a valid UUID", false, nil)
		return
	}

	var req reorderContractFilesRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	a.mu.Lock()
	defer a.mu.Unlock()

	item, ok := a.contracts[contractID]
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "contract not found", false, nil)
		return
	}

	if len(req.FileIDs) != len(item.FileIDs) {
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
			writeError(w, http.StatusBadRequest, "invalid_argument", "file_ids contains an unknown file id", false, nil)
			return
		}
		if _, ok := seen[id]; ok {
			writeError(w, http.StatusBadRequest, "invalid_argument", "file_ids must not contain duplicates", false, nil)
			return
		}
		seen[id] = struct{}{}
	}

	item.FileIDs = append([]string{}, req.FileIDs...)
	item.UpdatedAt = time.Now().UTC()
	a.contracts[contractID] = item
	a.emitAuditEvent("contract.files_reordered", "contract", contractID, map[string]any{"file_count": len(item.FileIDs)})

	files := make([]documentResponse, 0, len(item.FileIDs))
	for _, fileID := range item.FileIDs {
		if doc, exists := a.documents[fileID]; exists {
			files = append(files, mapDocument(doc))
		}
	}
	writeJSON(w, http.StatusOK, mapContract(item, files))
}
