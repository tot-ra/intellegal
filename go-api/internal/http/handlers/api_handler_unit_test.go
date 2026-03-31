//go:build !integration

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/models"
)

type noopLogger struct{}

func (noopLogger) Info(string, ...any)  {}
func (noopLogger) Warn(string, ...any)  {}
func (noopLogger) Error(string, ...any) {}

type stubDocumentStore struct {
	getBody      []byte
	getErr       error
	gotGetKeys   []string
	deleteErr    error
	deletedKeys  []string
	deletedCalls int
}

type searchCapturingAIClient struct {
	req ai.SearchSectionsRequest
	ctx context.Context
}

func (s *searchCapturingAIClient) AnalyzeClause(context.Context, ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (s *searchCapturingAIClient) AnalyzeCompanyName(context.Context, ai.AnalyzeCompanyNameRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (s *searchCapturingAIClient) AnalyzeLLMReview(context.Context, ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (s *searchCapturingAIClient) ContractChat(context.Context, ai.ContractChatRequest) (ai.ContractChatResult, error) {
	return ai.ContractChatResult{}, nil
}

func (s *searchCapturingAIClient) Extract(context.Context, ai.ExtractRequest) (ai.ExtractResult, error) {
	return ai.ExtractResult{}, nil
}

func (s *searchCapturingAIClient) Index(context.Context, ai.IndexRequest) (ai.IndexResult, error) {
	return ai.IndexResult{}, nil
}

func (s *searchCapturingAIClient) SearchSections(ctx context.Context, req ai.SearchSectionsRequest) (ai.SearchSectionsResult, error) {
	s.ctx = ctx
	s.req = req
	return ai.SearchSectionsResult{
		Items: []ai.SearchSectionsResultItem{
			{
				DocumentID:  req.DocumentIDs[0],
				PageNumber:  2,
				ChunkID:     "3",
				Score:       0.91,
				SnippetText: "payment terms apply",
			},
		},
	}, nil
}

func (s *stubDocumentStore) Put(_ context.Context, key string, _ io.Reader) (string, error) {
	return "file:///" + key, nil
}

func (s *stubDocumentStore) Get(_ context.Context, key string) (io.ReadCloser, error) {
	s.gotGetKeys = append(s.gotGetKeys, key)
	if s.getErr != nil {
		return nil, s.getErr
	}
	return io.NopCloser(bytes.NewReader(s.getBody)), nil
}

func (s *stubDocumentStore) Delete(_ context.Context, key string) error {
	s.deletedCalls++
	s.deletedKeys = append(s.deletedKeys, key)
	if s.deleteErr != nil {
		return s.deleteErr
	}
	return nil
}

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

func useInMemoryReaders(api *API) {
	api.contractsModel = &inMemoryContractReader{api: api}
	api.documentsModel = &inMemoryDocumentReader{api: api}
}

func TestCreateDocument_ReturnsBadRequestForUnsupportedMIMEType(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "contract.txt",
		"mime_type":      "text/plain",
		"content_base64": "dGVzdA==",
	}, api.CreateDocument)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}

	// Assert
	body := decodeJSONBody(t, resp)
	if body.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument error code, got %q", body.Error.Code)
	}
}

func TestCreateDocument_StoresNormalizedTags(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "contract.pdf",
		"mime_type":      "application/pdf",
		"content_base64": "dGVzdA==",
		"tags":           []string{"  MSA  ", "Finance", "finance", "", "2026"},
	}, api.CreateDocument)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.Code)
	}

	// Assert
	var body struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	decodeJSONBodyInto(t, resp, &body)
	if body.ID == "" {
		t.Fatal("expected created document id")
	}
	if len(body.Tags) != 3 {
		t.Fatalf("expected 3 normalized tags, got %d", len(body.Tags))
	}
	if body.Tags[0] != "MSA" || body.Tags[1] != "Finance" || body.Tags[2] != "2026" {
		t.Fatalf("unexpected tags: %#v", body.Tags)
	}
}

func TestCreateDocument_AcceptsPNGFiles(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "scan.png",
		"mime_type":      "image/png",
		"content_base64": "dGVzdA==",
	}, api.CreateDocument)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.Code)
	}

	// Assert
	var body struct {
		MIMEType string `json:"mime_type"`
	}
	decodeJSONBodyInto(t, resp, &body)
	if body.MIMEType != "image/png" {
		t.Fatalf("expected image/png mime type, got %q", body.MIMEType)
	}
}

func TestCreateDocument_AcceptsDOCXFiles(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "contract.docx",
		"mime_type":      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"content_base64": "dGVzdA==",
	}, api.CreateDocument)

	if resp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", resp.Code)
	}

	// Assert
	var body struct {
		MIMEType string `json:"mime_type"`
	}
	decodeJSONBodyInto(t, resp, &body)
	if body.MIMEType != "application/vnd.openxmlformats-officedocument.wordprocessingml.document" {
		t.Fatalf("expected DOCX mime type, got %q", body.MIMEType)
	}
}

func TestContract_SupportsMultipleFilesAndReordering(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	// Act
	createContractResp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts", map[string]any{
		"name": "MSA 2026",
	}, api.CreateContract)
	if createContractResp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createContractResp.Code)
	}

	// Assert
	var contractBody struct {
		ID string `json:"id"`
	}
	decodeJSONBodyInto(t, createContractResp, &contractBody)
	if contractBody.ID == "" {
		t.Fatal("expected contract id")
	}

	addFile := func(filename, mime string) string {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+contractBody.ID+"/files", bytes.NewReader([]byte(`{
			"filename":"`+filename+`",
			"mime_type":"`+mime+`",
			"content_base64":"dGVzdA=="
		}`)))
		req.Header.Set("Content-Type", "application/json")
		req.SetPathValue("contract_id", contractBody.ID)
		w := httptest.NewRecorder()
		api.AddContractFile(w, req)
		if w.Code != http.StatusCreated {
			t.Fatalf("expected 201 while adding file, got %d", w.Code)
		}
		var out struct {
			ID string `json:"id"`
		}
		decodeJSONBodyInto(t, w, &out)
		return out.ID
	}

	firstFileID := addFile("page-1.pdf", "application/pdf")
	secondFileID := addFile("page-2.png", "image/png")

	getReq := httptest.NewRequest(http.MethodGet, "/api/v1/contracts/"+contractBody.ID, nil)
	getReq.SetPathValue("contract_id", contractBody.ID)
	getResp := httptest.NewRecorder()
	api.GetContract(getResp, getReq)
	if getResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", getResp.Code)
	}

	var detail struct {
		FileCount int `json:"file_count"`
		Files     []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	decodeJSONBodyInto(t, getResp, &detail)
	if detail.FileCount != 2 {
		t.Fatalf("expected 2 files, got %d", detail.FileCount)
	}
	if len(detail.Files) != 2 || detail.Files[0].ID != firstFileID || detail.Files[1].ID != secondFileID {
		t.Fatalf("unexpected original file order: %#v", detail.Files)
	}

	reorderReq := httptest.NewRequest(http.MethodPatch, "/api/v1/contracts/"+contractBody.ID+"/files/order", bytes.NewReader([]byte(`{
		"file_ids":["`+secondFileID+`","`+firstFileID+`"]
	}`)))
	reorderReq.Header.Set("Content-Type", "application/json")
	reorderReq.SetPathValue("contract_id", contractBody.ID)
	reorderResp := httptest.NewRecorder()
	api.ReorderContractFiles(reorderResp, reorderReq)
	if reorderResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", reorderResp.Code)
	}

	var reordered struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	decodeJSONBodyInto(t, reorderResp, &reordered)
	if len(reordered.Files) != 2 || reordered.Files[0].ID != secondFileID || reordered.Files[1].ID != firstFileID {
		t.Fatalf("unexpected reordered file order: %#v", reordered.Files)
	}
}

func TestGetDocumentContent_StreamsOriginalFile(t *testing.T) {
	// Arrange
	store := &stubDocumentStore{getBody: []byte("%PDF-test")}
	api := NewAPI(noopLogger{}, nil, store, nil)
	useInMemoryReaders(api)

	documentID := "00000000-0000-4000-8000-000000000010"
	api.documents[documentID] = document{
		ID:         documentID,
		Filename:   "contract.pdf",
		MIMEType:   "application/pdf",
		StorageKey: "documents/contract.pdf",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/"+documentID+"/content", nil)
	req.SetPathValue("document_id", documentID)
	resp := httptest.NewRecorder()

	// Act
	api.GetDocumentContent(resp, req)

	// Assert
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if got := resp.Header().Get("Content-Type"); got != "application/pdf" {
		t.Fatalf("expected application/pdf content type, got %q", got)
	}
	if got := resp.Header().Get("Content-Disposition"); got != `inline; filename="contract.pdf"` {
		t.Fatalf("unexpected content disposition: %q", got)
	}
	if body := resp.Body.String(); body != "%PDF-test" {
		t.Fatalf("unexpected body: %q", body)
	}
	if len(store.gotGetKeys) != 1 || store.gotGetKeys[0] != "documents/contract.pdf" {
		t.Fatalf("unexpected storage keys: %#v", store.gotGetKeys)
	}
}

func TestGetDocumentContent_ReturnsBadGatewayWhenStorageReadFails(t *testing.T) {
	// Arrange
	store := &stubDocumentStore{getErr: errors.New("boom")}
	api := NewAPI(noopLogger{}, nil, store, nil)
	useInMemoryReaders(api)

	documentID := "00000000-0000-4000-8000-000000000011"
	api.documents[documentID] = document{
		ID:         documentID,
		Filename:   "scan.png",
		MIMEType:   "image/png",
		StorageKey: "documents/scan.png",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/"+documentID+"/content", nil)
	req.SetPathValue("document_id", documentID)
	resp := httptest.NewRecorder()

	// Act
	api.GetDocumentContent(resp, req)

	// Assert
	if resp.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", resp.Code)
	}
	body := decodeJSONBody(t, resp)
	if body.Error.Code != "storage_unavailable" {
		t.Fatalf("expected storage_unavailable error code, got %q", body.Error.Code)
	}
}

func TestUpdateContract_UpdatesNameAndTags(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	createResp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts", map[string]any{
		"name": "Original Name",
		"tags": []string{"  legal  ", "Finance", "finance"},
	}, api.CreateContract)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d", createResp.Code)
	}

	var created struct {
		ID string `json:"id"`
	}
	decodeJSONBodyInto(t, createResp, &created)

	updateReq := httptest.NewRequest(http.MethodPatch, "/api/v1/contracts/"+created.ID, bytes.NewReader([]byte(`{
		"name": "  Updated Name ",
		"tags": ["MSA", " procurement ", "msa"]
	}`)))
	updateReq.Header.Set("Content-Type", "application/json")
	updateReq.SetPathValue("contract_id", created.ID)
	updateResp := httptest.NewRecorder()

	api.UpdateContract(updateResp, updateReq)

	// Assert
	if updateResp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", updateResp.Code)
	}

	var body struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	decodeJSONBodyInto(t, updateResp, &body)
	if body.Name != "Updated Name" {
		t.Fatalf("expected trimmed updated name, got %q", body.Name)
	}
	if len(body.Tags) != 2 || body.Tags[0] != "MSA" || body.Tags[1] != "procurement" {
		t.Fatalf("unexpected updated tags: %#v", body.Tags)
	}
}

func TestUpdateContract_ReturnsBadRequestForEmptyPayload(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	contractID := "00000000-0000-4000-8000-000000000021"
	api.contracts[contractID] = contract{
		ID:        contractID,
		Name:      "MSA",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/contracts/"+contractID, bytes.NewReader([]byte(`{}`)))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("contract_id", contractID)
	w := httptest.NewRecorder()

	// Act
	api.UpdateContract(w, req)

	// Assert
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", w.Code)
	}
	body := decodeJSONBody(t, w)
	if body.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument, got %q", body.Error.Code)
	}
}

func TestCreateClauseCheck_ReturnsBadRequestForShortText(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/checks/clause-presence", map[string]any{
		"required_clause_text": "abc",
	}, api.CreateClauseCheck)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}

	// Assert
	body := decodeJSONBody(t, resp)
	if body.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument error code, got %q", body.Error.Code)
	}
}

func TestCreateCompanyNameCheck_ReturnsBadRequestForShortOldCompanyName(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/checks/company-name", map[string]any{
		"old_company_name": " ",
	}, api.CreateCompanyNameCheck)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}

	// Assert
	body := decodeJSONBody(t, resp)
	if body.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument error code, got %q", body.Error.Code)
	}
}

func TestCreateClauseCheck_ReturnsBadRequestForUnknownDocumentID(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/checks/clause-presence", map[string]any{
		"document_ids":         []string{"00000000-0000-4000-8000-000000000001"},
		"required_clause_text": "payment terms are required",
	}, api.CreateClauseCheck)

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}

	// Assert
	body := decodeJSONBody(t, resp)
	if body.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument error code, got %q", body.Error.Code)
	}
}

func TestGetCheckResults_ReturnsConflictWhenCheckIsNotCompleted(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	checkID := "00000000-0000-4000-8000-000000000001"
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusQueued,
		CheckType:   checkTypeClause,
		RequestedAt: time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/checks/"+checkID+"/results", nil)
	req.SetPathValue("check_id", checkID)
	w := httptest.NewRecorder()

	// Act
	api.GetCheckResults(w, req)

	// Assert
	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d", w.Code)
	}

	body := decodeJSONBody(t, w)
	if body.Error.Code != "results_not_ready" {
		t.Fatalf("expected results_not_ready, got %q", body.Error.Code)
	}
}

func TestDeleteDocument_RemovesDocumentAndRelatedData(t *testing.T) {
	// Arrange
	store := &stubDocumentStore{}
	api := NewAPI(noopLogger{}, nil, store, nil)

	documentID := "00000000-0000-4000-8000-000000000031"
	checkID := "00000000-0000-4000-8000-000000000032"
	api.documents[documentID] = document{
		ID:         documentID,
		Filename:   "contract.pdf",
		MIMEType:   "application/pdf",
		StorageKey: "documents/test.pdf",
		StorageURI: "file:///documents/test.pdf",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusCompleted,
		CheckType:   checkTypeClause,
		RequestedAt: time.Now().UTC(),
		DocumentIDs: []string{documentID},
	}
	api.idempotency[checkTypeClause+":idem-1"] = idempotencyRecord{
		PayloadHash: "abc",
		CheckID:     checkID,
	}
	api.copyEvents["event-1"] = externalCopyEvent{
		ID:         "event-1",
		DocumentID: documentID,
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/documents/"+documentID, nil)
	req.SetPathValue("document_id", documentID)
	w := httptest.NewRecorder()

	// Act
	api.DeleteDocument(w, req)

	// Assert
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if store.deletedCalls != 1 {
		t.Fatalf("expected one storage delete call, got %d", store.deletedCalls)
	}
	if len(store.deletedKeys) != 1 || store.deletedKeys[0] != "documents/test.pdf" {
		t.Fatalf("unexpected deleted keys: %#v", store.deletedKeys)
	}
	if _, ok := api.documents[documentID]; ok {
		t.Fatal("expected document to be removed")
	}
	if _, ok := api.checks[checkID]; ok {
		t.Fatal("expected related check to be removed")
	}
	if _, ok := api.idempotency[checkTypeClause+":idem-1"]; ok {
		t.Fatal("expected related idempotency record to be removed")
	}
	if _, ok := api.copyEvents["event-1"]; ok {
		t.Fatal("expected related copy event to be removed")
	}
}

func TestDeleteDocument_KeepsMetadataWhenStorageDeleteFails(t *testing.T) {
	// Arrange
	store := &stubDocumentStore{deleteErr: errors.New("storage is down")}
	api := NewAPI(noopLogger{}, nil, store, nil)

	documentID := "00000000-0000-4000-8000-000000000041"
	api.documents[documentID] = document{
		ID:         documentID,
		Filename:   "contract.pdf",
		MIMEType:   "application/pdf",
		StorageKey: "documents/fail.pdf",
		StorageURI: "file:///documents/fail.pdf",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/documents/"+documentID, nil)
	req.SetPathValue("document_id", documentID)
	w := httptest.NewRecorder()

	// Act
	api.DeleteDocument(w, req)

	// Assert
	if w.Code != http.StatusBadGateway {
		t.Fatalf("expected 502, got %d", w.Code)
	}
	if _, ok := api.documents[documentID]; !ok {
		t.Fatal("expected document to remain when storage delete fails")
	}
}

func TestDeleteContract_RemovesRelatedChecksAndCopyEvents(t *testing.T) {
	// Arrange
	store := &stubDocumentStore{}
	api := NewAPI(noopLogger{}, nil, store, nil)

	contractID := "00000000-0000-4000-8000-000000000061"
	firstDocumentID := "00000000-0000-4000-8000-000000000062"
	secondDocumentID := "00000000-0000-4000-8000-000000000063"
	checkID := "00000000-0000-4000-8000-000000000064"

	api.contracts[contractID] = contract{
		ID:        contractID,
		Name:      "MSA",
		FileIDs:   []string{firstDocumentID, secondDocumentID},
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}
	api.documents[firstDocumentID] = document{
		ID:         firstDocumentID,
		ContractID: contractID,
		Filename:   "contract-a.pdf",
		MIMEType:   "application/pdf",
		StorageKey: "documents/contract-a.pdf",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	api.documents[secondDocumentID] = document{
		ID:         secondDocumentID,
		ContractID: contractID,
		Filename:   "contract-b.pdf",
		MIMEType:   "application/pdf",
		StorageKey: "documents/contract-b.pdf",
		CreatedAt:  time.Now().UTC(),
		UpdatedAt:  time.Now().UTC(),
	}
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusCompleted,
		CheckType:   checkTypeClause,
		RequestedAt: time.Now().UTC(),
		DocumentIDs: []string{firstDocumentID, secondDocumentID},
	}
	api.idempotency[checkTypeClause+":idem-contract-delete"] = idempotencyRecord{
		PayloadHash: "hash",
		CheckID:     checkID,
	}
	api.copyEvents["event-contract-1"] = externalCopyEvent{ID: "event-contract-1", DocumentID: firstDocumentID}
	api.copyEvents["event-contract-2"] = externalCopyEvent{ID: "event-contract-2", DocumentID: secondDocumentID}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/contracts/"+contractID, nil)
	req.SetPathValue("contract_id", contractID)
	w := httptest.NewRecorder()

	// Act
	api.DeleteContract(w, req)

	// Assert
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204, got %d", w.Code)
	}
	if len(store.deletedKeys) != 2 {
		t.Fatalf("expected two storage deletes, got %#v", store.deletedKeys)
	}
	if _, ok := api.contracts[contractID]; ok {
		t.Fatal("expected contract to be removed")
	}
	if _, ok := api.documents[firstDocumentID]; ok {
		t.Fatal("expected first document to be removed")
	}
	if _, ok := api.documents[secondDocumentID]; ok {
		t.Fatal("expected second document to be removed")
	}
	if _, ok := api.checks[checkID]; ok {
		t.Fatal("expected related check to be removed")
	}
	if _, ok := api.idempotency[checkTypeClause+":idem-contract-delete"]; ok {
		t.Fatal("expected related idempotency key to be removed")
	}
	if _, ok := api.copyEvents["event-contract-1"]; ok {
		t.Fatal("expected first copy event to be removed")
	}
	if _, ok := api.copyEvents["event-contract-2"]; ok {
		t.Fatal("expected second copy event to be removed")
	}
}

func TestListDocuments_FiltersByTags(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	create := func(tags []string) {
		resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
			"filename":       "contract.pdf",
			"mime_type":      "application/pdf",
			"content_base64": "dGVzdA==",
			"tags":           tags,
		}, api.CreateDocument)
		if resp.Code != http.StatusCreated {
			t.Fatalf("expected 201, got %d", resp.Code)
		}
	}

	create([]string{"Finance", "MSA"})
	create([]string{"Vendor"})
	create([]string{"NDA", "Legal"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents?tag=msa&tag=vendor", nil)
	w := httptest.NewRecorder()

	// Act
	api.ListDocuments(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		Total int `json:"total"`
		Items []struct {
			Tags []string `json:"tags"`
		} `json:"items"`
	}
	decodeJSONBodyInto(t, w, &body)

	if body.Total != 2 {
		t.Fatalf("expected 2 documents for OR tag filter, got %d", body.Total)
	}
}

func TestGetDocumentText_ReturnsExtractedText(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)
	documentID := "00000000-0000-4000-8000-000000000051"
	api.documents[documentID] = document{
		ID:            documentID,
		Filename:      "master-service-agreement.pdf",
		MIMEType:      "application/pdf",
		ExtractedText: "Section 1. Parties\nAcme LLC and Vendor Ltd.",
		CreatedAt:     time.Now().UTC(),
		UpdatedAt:     time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents/"+documentID+"/text", nil)
	req.SetPathValue("document_id", documentID)
	w := httptest.NewRecorder()

	// Act
	api.GetDocumentText(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var body struct {
		DocumentID string `json:"document_id"`
		HasText    bool   `json:"has_text"`
		Text       string `json:"text"`
	}
	decodeJSONBodyInto(t, w, &body)

	if body.DocumentID != documentID {
		t.Fatalf("expected document id %q, got %q", documentID, body.DocumentID)
	}
	if !body.HasText {
		t.Fatal("expected has_text=true")
	}
	if body.Text == "" {
		t.Fatal("expected non-empty extracted text")
	}
}

func TestSearchContracts_ReturnsBadRequestForInvalidStrategy(t *testing.T) {
	// Arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text": "payment terms",
		"strategy":   "hybrid",
	}, api.SearchContracts)

	// Assert
	if resp.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", resp.Code)
	}
	body := decodeJSONBody(t, resp)
	if body.Error.Code != "invalid_argument" {
		t.Fatalf("expected invalid_argument, got %q", body.Error.Code)
	}
}

func TestSearchContracts_PassesStrategyToAI(t *testing.T) {
	// Arrange
	aiClient := &searchCapturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)
	documentID := "00000000-0000-4000-8000-000000000071"
	api.documents[documentID] = document{
		ID:        documentID,
		Filename:  "contract.pdf",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	// Act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text": "payment terms",
		"strategy":   "strict",
	}, api.SearchContracts)

	// Assert
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", resp.Code)
	}
	if aiClient.req.Strategy != "strict" {
		t.Fatalf("expected strict strategy, got %q", aiClient.req.Strategy)
	}
	if len(aiClient.req.DocumentIDs) != 1 || aiClient.req.DocumentIDs[0] != documentID {
		t.Fatalf("unexpected document ids: %#v", aiClient.req.DocumentIDs)
	}
}

func TestSearchContracts_PropagatesRequestContext(t *testing.T) {
	// Arrange
	aiClient := &searchCapturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)
	documentID := "00000000-0000-4000-8000-000000000072"
	api.documents[documentID] = document{
		ID:        documentID,
		Filename:  "contract.pdf",
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/search", bytes.NewReader([]byte(`{
		"query_text":"payment terms"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	type ctxKey string
	ctx := context.WithValue(req.Context(), ctxKey("trace_id"), "trace-123")
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	// Act
	api.SearchContracts(w, req)

	// Assert
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if got := aiClient.ctx.Value(ctxKey("trace_id")); got != "trace-123" {
		t.Fatalf("expected propagated request context value, got %#v", got)
	}
}

type errorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func performJSONRequest(t *testing.T, method, path string, payload any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func decodeJSONBody(t *testing.T, resp *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var out errorResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return out
}

func decodeJSONBodyInto(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil && err != io.EOF {
		t.Fatal(err)
	}
}
