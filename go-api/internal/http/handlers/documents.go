package handlers

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/checksum"
	"legal-doc-intel/go-api/internal/db"
	"legal-doc-intel/go-api/internal/externalcopy"
	"legal-doc-intel/go-api/internal/http/middleware"
	"legal-doc-intel/go-api/internal/ids"
	"legal-doc-intel/go-api/internal/models"
)

func (a *API) CreateDocument(w http.ResponseWriter, r *http.Request) {
	var req createDocumentRequest
	if !decodeJSON(w, r, &req) {
		return
	}

	doc, err := a.createDocumentFromRequest(r.Context(), req, middleware.GetRequestID(r.Context()))
	if err != nil {
		a.writeCreateDocumentError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, mapDocument(doc))
}

func (a *API) ListDocuments(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	status := strings.TrimSpace(q.Get("status"))
	if status != "" {
		if _, ok := validDocStatuses[status]; !ok {
			writeError(w, http.StatusBadRequest, "invalid_argument", "invalid status filter", false, nil)
			return
		}
	}

	sourceType := strings.TrimSpace(q.Get("source_type"))
	if sourceType != "" {
		if _, ok := validSourceTypes[sourceType]; !ok {
			writeError(w, http.StatusBadRequest, "invalid_argument", "invalid source_type filter", false, nil)
			return
		}
	}
	tagFilters, err := parseTagFilters(q)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
		return
	}

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

	if a.documentsModel == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	items, total, err := a.documentsModel.List(r.Context(), models.DocumentsListFilter{
		Status:     status,
		SourceType: sourceType,
		Tags:       tagFilters,
		Limit:      limit,
		Offset:     offset,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("list documents query failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load documents", true, nil)
		return
	}

	respItems := make([]documentResponse, 0, len(items))
	for _, doc := range items {
		respItems = append(respItems, mapDocument(documentFromModel(doc)))
	}
	writeJSON(w, http.StatusOK, documentListResponse{Items: respItems, Limit: limit, Offset: offset, Total: total})
}

func (a *API) GetDocument(w http.ResponseWriter, r *http.Request) {
	documentID := pathParam(r, "document_id")
	if !ids.IsUUID(documentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "document_id must be a valid UUID", false, nil)
		return
	}

	if a.documentsModel == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	doc, ok, err := a.documentsModel.Get(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("get document query failed", "document_id", documentID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document", true, nil)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "document not found", false, nil)
		return
	}

	writeJSON(w, http.StatusOK, mapDocument(documentFromModel(doc)))
}

func (a *API) DeleteDocument(w http.ResponseWriter, r *http.Request) {
	documentID := pathParam(r, "document_id")
	if !ids.IsUUID(documentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "document_id must be a valid UUID", false, nil)
		return
	}

	a.mu.RLock()
	doc, ok := a.documents[documentID]
	a.mu.RUnlock()
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "document not found", false, nil)
		return
	}

	if err := a.store.Delete(r.Context(), doc.StorageKey); err != nil {
		a.logger.Error("document storage delete failed", "document_id", documentID, "error", err)
		writeError(w, http.StatusBadGateway, "storage_unavailable", "failed to delete document asset", true, nil)
		return
	}

	if err := a.deleteDocumentState(r.Context(), documentID); err != nil {
		a.logger.Error("document metadata delete failed", "document_id", documentID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to delete document metadata", true, nil)
		return
	}

	a.mu.Lock()
	if _, exists := a.documents[documentID]; !exists {
		a.mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
		return
	}

	delete(a.documents, documentID)
	if doc.ContractID != "" {
		if item, exists := a.contracts[doc.ContractID]; exists {
			filtered := make([]string, 0, len(item.FileIDs))
			for _, id := range item.FileIDs {
				if id != documentID {
					filtered = append(filtered, id)
				}
			}
			item.FileIDs = filtered
			item.UpdatedAt = time.Now().UTC()
			a.contracts[doc.ContractID] = item
		}
	}

	deletedChecks := make(map[string]struct{})
	for checkID, run := range a.checks {
		if containsString(run.DocumentIDs, documentID) {
			delete(a.checks, checkID)
			deletedChecks[checkID] = struct{}{}
		}
	}

	for key, rec := range a.idempotency {
		if _, ok := deletedChecks[rec.CheckID]; ok {
			delete(a.idempotency, key)
		}
	}

	deletedCopyEvents := 0
	for eventID, event := range a.copyEvents {
		if event.DocumentID == documentID {
			delete(a.copyEvents, eventID)
			deletedCopyEvents++
		}
	}
	a.mu.Unlock()

	if doc.ContractID != "" {
		a.mu.RLock()
		item, exists := a.contracts[doc.ContractID]
		a.mu.RUnlock()
		if exists {
			_ = a.persistContract(context.Background(), item)
		}
	}

	a.emitAuditEvent("document.deleted", "document", documentID, map[string]any{
		"checks_deleted":      len(deletedChecks),
		"copy_events_deleted": deletedCopyEvents,
	})
	w.WriteHeader(http.StatusNoContent)
}

func (a *API) GetDocumentText(w http.ResponseWriter, r *http.Request) {
	documentID := pathParam(r, "document_id")
	if !ids.IsUUID(documentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "document_id must be a valid UUID", false, nil)
		return
	}

	if a.documentsModel == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	doc, ok, err := a.documentsModel.Get(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("get document text query failed", "document_id", documentID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document", true, nil)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "document not found", false, nil)
		return
	}

	text := strings.TrimSpace(doc.ExtractedText)
	writeJSON(w, http.StatusOK, documentTextResponse{
		DocumentID: doc.ID,
		Filename:   doc.Filename,
		Text:       text,
		HasText:    text != "",
	})
}

func (a *API) GetDocumentContent(w http.ResponseWriter, r *http.Request) {
	documentID := pathParam(r, "document_id")
	if !ids.IsUUID(documentID) {
		writeError(w, http.StatusBadRequest, "invalid_argument", "document_id must be a valid UUID", false, nil)
		return
	}

	if a.documentsModel == nil {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	doc, ok, err := a.documentsModel.Get(r.Context(), documentID)
	if err != nil {
		if errors.Is(err, db.ErrNotConfigured) {
			writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
			return
		}
		a.logger.Error("get document content query failed", "document_id", documentID, "error", err)
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to load document", true, nil)
		return
	}
	if !ok {
		writeError(w, http.StatusNotFound, "not_found", "document not found", false, nil)
		return
	}

	reader, err := a.store.Get(r.Context(), doc.StorageKey)
	if err != nil {
		a.logger.Error("document storage read failed", "document_id", documentID, "error", err)
		writeError(w, http.StatusBadGateway, "storage_unavailable", "failed to read document asset", true, nil)
		return
	}
	defer reader.Close()

	w.Header().Set("Content-Type", doc.MIMEType)
	w.Header().Set("Content-Disposition", fmt.Sprintf("inline; filename=%q", doc.Filename))
	w.Header().Set("X-Content-Type-Options", "nosniff")
	if _, err := io.Copy(w, reader); err != nil {
		a.logger.Error("document content stream failed", "document_id", documentID, "error", err)
	}
}

func (a *API) writeCreateDocumentError(w http.ResponseWriter, err error) {
	if errors.Is(err, db.ErrNotConfigured) {
		writeError(w, http.StatusServiceUnavailable, "service_unavailable", "database is not configured", true, nil)
		return
	}

	switch err.Error() {
	case "filename is required", "unsupported mime_type", "content_base64 must be valid base64", "unsupported source_type":
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
	case "contract_id must be a valid UUID":
		writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
	case "contract not found":
		writeError(w, http.StatusNotFound, "not_found", err.Error(), false, nil)
	case "failed to persist document":
		writeError(w, http.StatusBadGateway, "storage_unavailable", err.Error(), true, nil)
	case "failed to extract document text", "failed to index document text":
		writeError(w, http.StatusBadGateway, "upstream_unavailable", err.Error(), true, nil)
	default:
		if strings.HasPrefix(err.Error(), "tag ") || strings.HasPrefix(err.Error(), "at most ") {
			writeError(w, http.StatusBadRequest, "invalid_argument", err.Error(), false, nil)
			return
		}
		writeError(w, http.StatusInternalServerError, "internal_error", "failed to create document", true, nil)
	}
}

func (a *API) createDocumentFromRequest(ctx context.Context, req createDocumentRequest, requestID string) (document, error) {
	if strings.TrimSpace(req.Filename) == "" {
		return document{}, errors.New("filename is required")
	}
	if _, ok := validDocumentMimes[req.MIMEType]; !ok {
		return document{}, errors.New("unsupported mime_type")
	}
	payload, err := base64.StdEncoding.DecodeString(req.ContentBase64)
	if err != nil {
		return document{}, errors.New("content_base64 must be valid base64")
	}

	sourceType := req.SourceType
	if sourceType == "" {
		sourceType = "upload"
	}
	if _, ok := validSourceTypes[sourceType]; !ok {
		return document{}, errors.New("unsupported source_type")
	}

	tags, err := normalizeTags(req.Tags)
	if err != nil {
		return document{}, err
	}

	contractID := strings.TrimSpace(req.ContractID)
	if contractID != "" {
		if !ids.IsUUID(contractID) {
			return document{}, errors.New("contract_id must be a valid UUID")
		}
		if a.contractsModel == nil {
			return document{}, db.ErrNotConfigured
		}
		_, exists, err := a.contractsModel.Get(ctx, contractID)
		if err != nil {
			return document{}, err
		}
		if !exists {
			return document{}, errors.New("contract not found")
		}
	}

	now := time.Now().UTC()
	docID := ids.NewUUID()
	checksum := checksum.SHA256Hex(payload)
	objectKey := fmt.Sprintf("documents/%s%s", docID, extensionForFilename(req.Filename, req.MIMEType))
	storageURI, err := a.store.Put(ctx, objectKey, bytes.NewReader(payload))
	if err != nil {
		a.logger.Error("document storage failed", "document_id", docID, "error", err)
		return document{}, errors.New("failed to persist document")
	}

	doc := document{
		ID:         docID,
		ContractID: contractID,
		SourceType: sourceType,
		SourceRef:  req.SourceRef,
		Tags:       tags,
		Filename:   req.Filename,
		MIMEType:   req.MIMEType,
		Status:     documentStatusProcessing,
		Checksum:   checksum,
		StorageKey: objectKey,
		StorageURI: storageURI,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	a.mu.Lock()
	a.documents[docID] = doc
	if contractID != "" {
		item := a.contracts[contractID]
		item.FileIDs = append(item.FileIDs, docID)
		item.UpdatedAt = now
		a.contracts[contractID] = item
	}
	a.mu.Unlock()
	if err := a.persistDocument(ctx, doc); err != nil {
		a.mu.Lock()
		delete(a.documents, docID)
		if contractID != "" {
			item := a.contracts[contractID]
			filtered := make([]string, 0, len(item.FileIDs))
			for _, id := range item.FileIDs {
				if id != docID {
					filtered = append(filtered, id)
				}
			}
			item.FileIDs = filtered
			a.contracts[contractID] = item
		}
		a.mu.Unlock()
		return document{}, errors.New("failed to persist document")
	}
	if contractID != "" {
		a.mu.RLock()
		item := a.contracts[contractID]
		a.mu.RUnlock()
		if err := a.persistContract(ctx, item); err != nil {
			return document{}, errors.New("failed to persist document")
		}
	}

	a.emitAuditEvent("document.created", "document", docID, map[string]any{
		"source_type": sourceType,
		"mime_type":   req.MIMEType,
		"checksum":    checksum,
		"tags":        tags,
		"contract_id": contractID,
	})

	extractResult, err := a.ai.Extract(ctx, ai.ExtractRequest{
		JobID:      ids.NewUUID(),
		RequestID:  requestID,
		DocumentID: docID,
		StorageURI: storageURI,
		MIMEType:   req.MIMEType,
	})
	if err != nil {
		a.markDocumentFailed(docID, err)
		return document{}, errors.New("failed to extract document text")
	}

	pages := make([]ai.IndexPageInput, 0, len(extractResult.Pages))
	for _, page := range extractResult.Pages {
		pages = append(pages, ai.IndexPageInput{
			PageNumber: page.PageNumber,
			Text:       page.Text,
		})
	}

	if _, err := a.ai.Index(ctx, ai.IndexRequest{
		JobID:           ids.NewUUID(),
		RequestID:       requestID,
		DocumentID:      docID,
		VersionChecksum: checksum,
		ExtractedText:   extractResult.Text,
		Pages:           pages,
		SourceURI:       storageURI,
		Reindex:         false,
	}); err != nil {
		a.markDocumentFailed(docID, err)
		return document{}, errors.New("failed to index document text")
	}

	doc.ExtractedText = combineExtractedText(extractResult)
	doc.UpdatedAt = time.Now().UTC()
	a.mu.Lock()
	a.documents[docID] = doc
	a.mu.Unlock()
	if err := a.persistDocument(ctx, doc); err != nil {
		return document{}, errors.New("failed to persist document")
	}

	doc = a.markDocumentIndexed(docID)
	a.emitAuditEvent("document.indexed", "document", docID, map[string]any{"status": doc.Status})
	a.enqueueExternalCopy(doc, requestID)
	return doc, nil
}

func combineExtractedText(result ai.ExtractResult) string {
	if strings.TrimSpace(result.Text) != "" {
		return strings.TrimSpace(result.Text)
	}
	if len(result.Pages) == 0 {
		return ""
	}
	parts := make([]string, 0, len(result.Pages))
	for _, page := range result.Pages {
		content := strings.TrimSpace(page.Text)
		if content == "" {
			continue
		}
		parts = append(parts, content)
	}
	return strings.TrimSpace(strings.Join(parts, "\n\n"))
}

func (a *API) markDocumentFailed(documentID string, err error) {
	a.mu.Lock()
	doc := a.documents[documentID]
	doc.Status = documentStatusFailed
	doc.UpdatedAt = time.Now().UTC()
	a.documents[documentID] = doc
	a.mu.Unlock()
	a.logger.Error("document processing failed", "document_id", documentID, "error", err)
	a.emitAuditEvent("document.failed", "document", documentID, map[string]any{"error": err.Error()})
	_ = a.persistDocument(context.Background(), doc)
}

func (a *API) markDocumentIndexed(documentID string) document {
	a.mu.Lock()
	doc := a.documents[documentID]
	doc.Status = documentStatusIndexed
	doc.UpdatedAt = time.Now().UTC()
	a.documents[documentID] = doc
	a.mu.Unlock()
	_ = a.persistDocument(context.Background(), doc)
	return doc
}

func (a *API) enqueueExternalCopy(doc document, requestID string) {
	if !a.copier.Enabled() {
		return
	}

	now := time.Now().UTC()
	eventID := ids.NewUUID()
	payload := map[string]any{
		"request_id":  requestID,
		"document_id": doc.ID,
		"filename":    doc.Filename,
		"mime_type":   doc.MIMEType,
		"checksum":    doc.Checksum,
		"storage_uri": doc.StorageURI,
	}

	a.mu.Lock()
	a.copyEvents[eventID] = externalCopyEvent{
		ID:             eventID,
		DocumentID:     doc.ID,
		TargetSystem:   "external_copy_api",
		Status:         "queued",
		RequestPayload: payload,
		CreatedAt:      now,
		UpdatedAt:      now,
	}
	event := a.copyEvents[eventID]
	a.mu.Unlock()
	if err := a.persistCopyEvent(context.Background(), event); err != nil {
		a.logger.Error("copy event persist failed", "event_id", eventID, "error", err)
	}

	a.emitAuditEvent("external_copy.queued", "document", doc.ID, map[string]any{"event_id": eventID})
	go a.runExternalCopy(eventID, doc, requestID)
}

func (a *API) runExternalCopy(eventID string, doc document, requestID string) {
	result, err := a.copier.CopyDocument(context.Background(), externalcopy.CopyRequest{
		RequestID:  requestID,
		DocumentID: doc.ID,
		Filename:   doc.Filename,
		MIMEType:   doc.MIMEType,
		Checksum:   doc.Checksum,
		StorageURI: doc.StorageURI,
		CreatedAt:  doc.CreatedAt.Format(time.RFC3339),
	})

	a.mu.Lock()
	event := a.copyEvents[eventID]
	event.UpdatedAt = time.Now().UTC()
	if err != nil {
		event.Status = "failed"
		event.ErrorMessage = err.Error()
		var callErr *externalcopy.CallError
		if errors.As(err, &callErr) {
			event.Attempts = callErr.Attempts
		}
		a.copyEvents[eventID] = event
		a.mu.Unlock()
		if persistErr := a.persistCopyEvent(context.Background(), event); persistErr != nil {
			a.logger.Error("copy event persist failed", "event_id", eventID, "error", persistErr)
		}
		a.emitAuditEvent("external_copy.failed", "document", doc.ID, map[string]any{
			"event_id": eventID,
			"error":    event.ErrorMessage,
			"attempts": event.Attempts,
		})
		a.logger.Error("external copy failed", "document_id", doc.ID, "event_id", eventID, "error", err)
		return
	}

	event.Status = "succeeded"
	event.Attempts = result.Attempts
	event.ResponseBody = result.Body
	a.copyEvents[eventID] = event
	a.mu.Unlock()
	if err := a.persistCopyEvent(context.Background(), event); err != nil {
		a.logger.Error("copy event persist failed", "event_id", eventID, "error", err)
	}

	a.emitAuditEvent("external_copy.succeeded", "document", doc.ID, map[string]any{
		"event_id": eventID,
		"attempts": result.Attempts,
	})
}
