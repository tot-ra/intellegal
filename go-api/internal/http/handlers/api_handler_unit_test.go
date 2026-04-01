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
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"legal-doc-intel/go-api/internal/ai"
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
	req            ai.SearchSectionsRequest
	searchRequests []ai.SearchSectionsRequest
	ctx            context.Context
	searchResponse ai.SearchSectionsResult
	chatReq        ai.ContractChatRequest
	chatResponse   ai.ContractChatResult
}

func (s *searchCapturingAIClient) AnalyzeClause(context.Context, ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (s *searchCapturingAIClient) AnalyzeLLMReview(context.Context, ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (s *searchCapturingAIClient) ContractChat(_ context.Context, req ai.ContractChatRequest) (ai.ContractChatResult, error) {
	s.chatReq = req
	if s.chatResponse.Answer != "" || len(s.chatResponse.Citations) > 0 {
		return s.chatResponse, nil
	}
	return ai.ContractChatResult{
		Answer: "Search-backed answer.",
		Citations: []ai.ContractChatCitation{
			{
				DocumentID:  req.Documents[0].DocumentID,
				SnippetText: "payment terms apply",
				Reason:      "Supports the answer",
			},
		},
	}, nil
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
	s.searchRequests = append(s.searchRequests, req)
	if len(s.searchResponse.Items) > 0 {
		return s.searchResponse, nil
	}
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

func useInMemoryReaders(api *API) {
	api.EnableInMemoryReadersForTesting()
}

func TestCreateDocument_ReturnsBadRequestForUnsupportedMIMEType(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "contract.txt",
		"mime_type":      "text/plain",
		"content_base64": "dGVzdA==",
	}, api.CreateDocument)

	// assert
	assert.Equal(t, http.StatusBadRequest, resp.Code)

	body := decodeJSONBody(t, resp)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestCreateDocument_StoresNormalizedTags(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "contract.pdf",
		"mime_type":      "application/pdf",
		"content_base64": "dGVzdA==",
		"tags":           []string{"  MSA  ", "Finance", "finance", "", "2026"},
	}, api.CreateDocument)

	assert.Equal(t, http.StatusCreated, resp.Code)

	// assert
	var body struct {
		ID   string   `json:"id"`
		Tags []string `json:"tags"`
	}
	decodeJSONBodyInto(t, resp, &body)
	assert.NotEmpty(t, body.ID)
	assert.Equal(t, []string{"MSA", "Finance", "2026"}, body.Tags)
}

func TestCreateDocument_AcceptsPNGFiles(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "scan.png",
		"mime_type":      "image/png",
		"content_base64": "dGVzdA==",
	}, api.CreateDocument)

	assert.Equal(t, http.StatusCreated, resp.Code)

	// assert
	var body struct {
		MIMEType string `json:"mime_type"`
	}
	decodeJSONBodyInto(t, resp, &body)
	assert.Equal(t, "image/png", body.MIMEType)
}

func TestCreateDocument_AcceptsDOCXFiles(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
		"filename":       "contract.docx",
		"mime_type":      "application/vnd.openxmlformats-officedocument.wordprocessingml.document",
		"content_base64": "dGVzdA==",
	}, api.CreateDocument)

	assert.Equal(t, http.StatusCreated, resp.Code)

	// assert
	var body struct {
		MIMEType string `json:"mime_type"`
	}
	decodeJSONBodyInto(t, resp, &body)
	assert.Equal(t, "application/vnd.openxmlformats-officedocument.wordprocessingml.document", body.MIMEType)
}

func TestContract_SupportsMultipleFilesAndReordering(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	// act
	createContractResp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts", map[string]any{
		"name": "MSA 2026",
	}, api.CreateContract)
	assert.Equal(t, http.StatusCreated, createContractResp.Code)

	// assert
	var contractBody struct {
		ID       string `json:"id"`
		Language string `json:"language"`
	}
	decodeJSONBodyInto(t, createContractResp, &contractBody)
	assert.NotEmpty(t, contractBody.ID)
	assert.Equal(t, "eng", contractBody.Language)

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
		assert.Equal(t, http.StatusCreated, w.Code)
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
	assert.Equal(t, http.StatusOK, getResp.Code)

	var detail struct {
		FileCount int `json:"file_count"`
		Files     []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	decodeJSONBodyInto(t, getResp, &detail)
	assert.Equal(t, 2, detail.FileCount)
	require.Len(t, detail.Files, 2)
	assert.Equal(t, firstFileID, detail.Files[0].ID)
	assert.Equal(t, secondFileID, detail.Files[1].ID)

	reorderReq := httptest.NewRequest(http.MethodPatch, "/api/v1/contracts/"+contractBody.ID+"/files/order", bytes.NewReader([]byte(`{
		"file_ids":["`+secondFileID+`","`+firstFileID+`"]
	}`)))
	reorderReq.Header.Set("Content-Type", "application/json")
	reorderReq.SetPathValue("contract_id", contractBody.ID)
	reorderResp := httptest.NewRecorder()
	api.ReorderContractFiles(reorderResp, reorderReq)
	assert.Equal(t, http.StatusOK, reorderResp.Code)

	var reordered struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	decodeJSONBodyInto(t, reorderResp, &reordered)
	require.Len(t, reordered.Files, 2)
	assert.Equal(t, secondFileID, reordered.Files[0].ID)
	assert.Equal(t, firstFileID, reordered.Files[1].ID)
}

func TestGetDocumentContent_StreamsOriginalFile(t *testing.T) {
	// arrange
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

	// act
	api.GetDocumentContent(resp, req)

	// assert
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "application/pdf", resp.Header().Get("Content-Type"))
	assert.Equal(t, `inline; filename="contract.pdf"`, resp.Header().Get("Content-Disposition"))
	assert.Equal(t, "%PDF-test", resp.Body.String())
	assert.Equal(t, []string{"documents/contract.pdf"}, store.gotGetKeys)
}

func TestGetDocumentContent_ReturnsBadGatewayWhenStorageReadFails(t *testing.T) {
	// arrange
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

	// act
	api.GetDocumentContent(resp, req)

	// assert
	assert.Equal(t, http.StatusBadGateway, resp.Code)
	body := decodeJSONBody(t, resp)
	assert.Equal(t, "storage_unavailable", body.Error.Code)
}

func TestUpdateContract_UpdatesNameAndTags(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	createResp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts", map[string]any{
		"name": "Original Name",
		"tags": []string{"  legal  ", "Finance", "finance"},
	}, api.CreateContract)
	assert.Equal(t, http.StatusCreated, createResp.Code)

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

	// assert
	assert.Equal(t, http.StatusOK, updateResp.Code)

	var body struct {
		Name string   `json:"name"`
		Tags []string `json:"tags"`
	}
	decodeJSONBodyInto(t, updateResp, &body)
	assert.Equal(t, "Updated Name", body.Name)
	assert.Equal(t, []string{"MSA", "procurement"}, body.Tags)
}

func TestUpdateContract_ReturnsBadRequestForEmptyPayload(t *testing.T) {
	// arrange
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

	// act
	api.UpdateContract(w, req)

	// assert
	assert.Equal(t, http.StatusBadRequest, w.Code)
	body := decodeJSONBody(t, w)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestCreateClauseCheck_ReturnsBadRequestForShortText(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/checks/clause-presence", map[string]any{
		"required_clause_text": "abc",
	}, api.CreateClauseCheck)

	// assert
	assert.Equal(t, http.StatusBadRequest, resp.Code)

	body := decodeJSONBody(t, resp)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestCreateClauseCheck_ReturnsBadRequestForUnknownDocumentID(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/checks/clause-presence", map[string]any{
		"document_ids":         []string{"00000000-0000-4000-8000-000000000001"},
		"required_clause_text": "payment terms are required",
	}, api.CreateClauseCheck)

	// assert
	assert.Equal(t, http.StatusBadRequest, resp.Code)

	body := decodeJSONBody(t, resp)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestGetCheckResults_ReturnsConflictWhenCheckIsNotCompleted(t *testing.T) {
	// arrange
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

	// act
	api.GetCheckResults(w, req)

	// assert
	assert.Equal(t, http.StatusConflict, w.Code)

	body := decodeJSONBody(t, w)
	assert.Equal(t, "results_not_ready", body.Error.Code)
}

func TestDeleteCheck_RemovesCheckAndIdempotencyRecord(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	checkID := "00000000-0000-4000-8000-000000000021"
	api.checks[checkID] = checkRun{
		CheckID:     checkID,
		Status:      checkStatusCompleted,
		CheckType:   checkTypeClause,
		RequestedAt: time.Now().UTC(),
		DocumentIDs: []string{"00000000-0000-4000-8000-000000000022"},
	}
	api.idempotency[checkTypeClause+":idem-delete-check"] = idempotencyRecord{
		PayloadHash: "hash",
		CheckID:     checkID,
	}

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/guidelines/"+checkID, nil)
	req.SetPathValue("check_id", checkID)
	w := httptest.NewRecorder()

	// act
	api.DeleteCheck(w, req)

	// assert
	assert.Equal(t, http.StatusNoContent, w.Code)
	_, checkExists := api.checks[checkID]
	assert.False(t, checkExists)
	_, recordExists := api.idempotency[checkTypeClause+":idem-delete-check"]
	assert.False(t, recordExists)
}

func TestDeleteChecks_RemovesMultipleChecks(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	firstCheckID := "00000000-0000-4000-8000-000000000023"
	secondCheckID := "00000000-0000-4000-8000-000000000024"
	api.checks[firstCheckID] = checkRun{
		CheckID:     firstCheckID,
		Status:      checkStatusCompleted,
		CheckType:   checkTypeClause,
		RequestedAt: time.Now().UTC(),
	}
	api.checks[secondCheckID] = checkRun{
		CheckID:     secondCheckID,
		Status:      checkStatusCompleted,
		CheckType:   checkTypeLLMReview,
		RequestedAt: time.Now().UTC(),
	}
	api.idempotency[checkTypeClause+":idem-bulk-delete-1"] = idempotencyRecord{
		PayloadHash: "hash-1",
		CheckID:     firstCheckID,
	}
	api.idempotency[checkTypeLLMReview+":idem-bulk-delete-2"] = idempotencyRecord{
		PayloadHash: "hash-2",
		CheckID:     secondCheckID,
	}

	// act
	resp := performJSONRequest(t, http.MethodDelete, "/api/v1/guidelines", map[string]any{
		"check_ids": []string{firstCheckID, secondCheckID},
	}, api.DeleteChecks)

	// assert
	assert.Equal(t, http.StatusNoContent, resp.Code)
	_, firstCheckExists := api.checks[firstCheckID]
	_, secondCheckExists := api.checks[secondCheckID]
	_, firstRecordExists := api.idempotency[checkTypeClause+":idem-bulk-delete-1"]
	_, secondRecordExists := api.idempotency[checkTypeLLMReview+":idem-bulk-delete-2"]
	assert.False(t, firstCheckExists)
	assert.False(t, secondCheckExists)
	assert.False(t, firstRecordExists)
	assert.False(t, secondRecordExists)
}

func TestDeleteDocument_RemovesDocumentAndRelatedData(t *testing.T) {
	// arrange
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

	// act
	api.DeleteDocument(w, req)

	// assert
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Equal(t, 1, store.deletedCalls)
	assert.Equal(t, []string{"documents/test.pdf"}, store.deletedKeys)
	_, documentExists := api.documents[documentID]
	_, checkExists := api.checks[checkID]
	_, recordExists := api.idempotency[checkTypeClause+":idem-1"]
	_, copyEventExists := api.copyEvents["event-1"]
	assert.False(t, documentExists)
	assert.False(t, checkExists)
	assert.False(t, recordExists)
	assert.False(t, copyEventExists)
}

func TestDeleteDocument_KeepsMetadataWhenStorageDeleteFails(t *testing.T) {
	// arrange
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

	// act
	api.DeleteDocument(w, req)

	// assert
	assert.Equal(t, http.StatusBadGateway, w.Code)
	_, documentExists := api.documents[documentID]
	assert.True(t, documentExists)
}

func TestDeleteContract_RemovesRelatedChecksAndCopyEvents(t *testing.T) {
	// arrange
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

	// act
	api.DeleteContract(w, req)

	// assert
	assert.Equal(t, http.StatusNoContent, w.Code)
	assert.Len(t, store.deletedKeys, 2)
	_, contractExists := api.contracts[contractID]
	_, firstDocumentExists := api.documents[firstDocumentID]
	_, secondDocumentExists := api.documents[secondDocumentID]
	_, checkExists := api.checks[checkID]
	_, recordExists := api.idempotency[checkTypeClause+":idem-contract-delete"]
	_, firstCopyEventExists := api.copyEvents["event-contract-1"]
	_, secondCopyEventExists := api.copyEvents["event-contract-2"]
	assert.False(t, contractExists)
	assert.False(t, firstDocumentExists)
	assert.False(t, secondDocumentExists)
	assert.False(t, checkExists)
	assert.False(t, recordExists)
	assert.False(t, firstCopyEventExists)
	assert.False(t, secondCopyEventExists)
}

func TestListDocuments_FiltersByTags(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	create := func(tags []string) {
		resp := performJSONRequest(t, http.MethodPost, "/api/v1/documents", map[string]any{
			"filename":       "contract.pdf",
			"mime_type":      "application/pdf",
			"content_base64": "dGVzdA==",
			"tags":           tags,
		}, api.CreateDocument)
		assert.Equal(t, http.StatusCreated, resp.Code)
	}

	create([]string{"Finance", "MSA"})
	create([]string{"Vendor"})
	create([]string{"NDA", "Legal"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents?tag=msa&tag=vendor", nil)
	w := httptest.NewRecorder()

	// act
	api.ListDocuments(w, req)

	// assert
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Total int `json:"total"`
		Items []struct {
			Tags []string `json:"tags"`
		} `json:"items"`
	}
	decodeJSONBodyInto(t, w, &body)

	assert.Equal(t, 2, body.Total)
}

func TestGetDocumentText_ReturnsExtractedText(t *testing.T) {
	// arrange
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

	// act
	api.GetDocumentText(w, req)

	// assert
	assert.Equal(t, http.StatusOK, w.Code)

	var body struct {
		DocumentID string `json:"document_id"`
		HasText    bool   `json:"has_text"`
		Text       string `json:"text"`
	}
	decodeJSONBodyInto(t, w, &body)

	assert.Equal(t, documentID, body.DocumentID)
	assert.True(t, body.HasText)
	assert.NotEmpty(t, body.Text)
}

func TestSearchContracts_ReturnsBadRequestForInvalidStrategy(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text": "payment terms",
		"strategy":   "hybrid",
	}, api.SearchContracts)

	// assert
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	body := decodeJSONBody(t, resp)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestSearchContracts_PassesStrategyToAI(t *testing.T) {
	// arrange
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

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text": "payment terms",
		"strategy":   "strict",
	}, api.SearchContracts)

	// assert
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "strict", aiClient.req.Strategy)
	assert.Equal(t, "sections", aiClient.req.ResultMode)
	assert.Equal(t, []string{documentID}, aiClient.req.DocumentIDs)
}

func TestSearchContracts_ReturnsBadRequestForInvalidResultMode(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text":  "payment terms",
		"result_mode": "documents",
	}, api.SearchContracts)

	// assert
	assert.Equal(t, http.StatusBadRequest, resp.Code)
	body := decodeJSONBody(t, resp)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestSearchContracts_CollapsesResultsByContract(t *testing.T) {
	// arrange
	aiClient := &searchCapturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)
	now := time.Now().UTC()

	api.documents["doc-1"] = document{
		ID:         "doc-1",
		ContractID: "contract-1",
		Filename:   "alpha-main.pdf",
		CreatedAt:  now,
		UpdatedAt:  now,
	}
	api.documents["doc-2"] = document{
		ID:         "doc-2",
		ContractID: "contract-1",
		Filename:   "alpha-appendix.pdf",
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	aiClientResp := []ai.SearchSectionsResultItem{
		{DocumentID: "doc-1", PageNumber: 7, ChunkID: "12", Score: 0.61, SnippetText: "payment terms"},
		{DocumentID: "doc-2", PageNumber: 1, ChunkID: "2", Score: 0.94, SnippetText: "payment terms with appendix"},
	}
	aiClient.searchResponse = ai.SearchSectionsResult{Items: aiClientResp}

	// act
	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text":  "payment terms",
		"result_mode": "contracts",
		"limit":       3,
	}, api.SearchContracts)

	// assert
	assert.Equal(t, http.StatusOK, resp.Code)
	assert.Equal(t, "contracts", aiClient.req.ResultMode)
	assert.Equal(t, 15, aiClient.req.Limit)

	var body contractSearchResponse
	require.NoError(t, json.Unmarshal(resp.Body.Bytes(), &body))
	require.Len(t, body.Items, 1)
	assert.Equal(t, "doc-2", body.Items[0].DocumentID)
}

func TestSearchContracts_PropagatesRequestContext(t *testing.T) {
	// arrange
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

	// act
	api.SearchContracts(w, req)

	// assert
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "trace-123", aiClient.ctx.Value(ctxKey("trace_id")))
}

func TestChatContractSearch_UsesLatestUserMessageForRetrievalAndSelectedDocuments(t *testing.T) {
	aiClient := &searchCapturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)
	now := time.Now().UTC()
	documentID := "00000000-0000-4000-8000-000000000081"
	contractID := "00000000-0000-4000-8000-000000000082"

	api.contracts[contractID] = contract{
		ID:        contractID,
		Name:      "Alpha",
		FileIDs:   []string{documentID},
		CreatedAt: now,
		UpdatedAt: now,
	}
	api.documents[documentID] = document{
		ID:            documentID,
		ContractID:    contractID,
		Filename:      "alpha.pdf",
		ExtractedText: "Alpha contract payment terms apply with net 30 days.",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search/chat", map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "Show me payment terms"},
			{"role": "assistant", "content": "Looking at matches."},
			{"role": "user", "content": "Only strict matches please"},
		},
		"document_ids": []string{documentID},
		"limit":        3,
	}, api.ChatContractSearch)

	assert.Equal(t, http.StatusOK, resp.Code)
	require.Len(t, aiClient.searchRequests, 2)
	assert.Equal(t, "Only strict matches please", aiClient.searchRequests[0].QueryText)
	assert.Equal(t, []string{documentID}, aiClient.searchRequests[0].DocumentIDs)
	assert.Equal(t, "strict", aiClient.searchRequests[0].Strategy)
	assert.Equal(t, "semantic", aiClient.searchRequests[1].Strategy)
	assert.Equal(t, "search-results", aiClient.chatReq.ContractID)
	require.Len(t, aiClient.chatReq.Documents, 1)
	assert.Equal(t, documentID, aiClient.chatReq.Documents[0].DocumentID)
	assert.Contains(t, aiClient.chatReq.Documents[0].Text, "Alpha contract payment terms apply")

	var body contractChatResponse
	decodeJSONBodyInto(t, resp, &body)
	assert.Equal(t, "Search-backed answer.", body.Answer)
	require.Len(t, body.Citations, 1)
	assert.Equal(t, contractID, body.Citations[0].ContractID)
	assert.Equal(t, "alpha.pdf", body.Citations[0].Filename)
	require.Len(t, body.Results, 1)
	assert.Equal(t, contractID, body.Results[0].ContractID)
	assert.Equal(t, "Alpha", body.Results[0].ContractName)
	assert.Equal(t, documentID, body.Results[0].DocumentID)
}

func TestChatContractSearch_IgnoresLegacyStrategyOverrideAndStillRunsAutonomousRetrieval(t *testing.T) {
	aiClient := &searchCapturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)
	now := time.Now().UTC()
	documentID := "00000000-0000-4000-8000-000000000083"

	api.documents[documentID] = document{
		ID:            documentID,
		Filename:      "alpha.pdf",
		ExtractedText: "Payment terms apply.",
		CreatedAt:     now,
		UpdatedAt:     now,
	}

	resp := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search/chat", map[string]any{
		"messages": []map[string]string{
			{"role": "user", "content": "payment terms"},
		},
		"strategy": "hybrid",
	}, api.ChatContractSearch)

	assert.Equal(t, http.StatusOK, resp.Code)
	require.Len(t, aiClient.searchRequests, 2)
	assert.Equal(t, "strict", aiClient.searchRequests[0].Strategy)
	assert.Equal(t, "semantic", aiClient.searchRequests[1].Strategy)
}

type errorResponse struct {
	Error struct {
		Code string `json:"code"`
	} `json:"error"`
}

func performJSONRequest(t *testing.T, method, path string, payload any, handler http.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	data, err := json.Marshal(payload)
	require.NoError(t, err)
	req := httptest.NewRequest(method, path, bytes.NewReader(data))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	handler(w, req)
	return w
}

func decodeJSONBody(t *testing.T, resp *httptest.ResponseRecorder) errorResponse {
	t.Helper()
	var out errorResponse
	err := json.NewDecoder(resp.Body).Decode(&out)
	require.True(t, err == nil || err == io.EOF)
	return out
}

func decodeJSONBodyInto(t *testing.T, resp *httptest.ResponseRecorder, out any) {
	t.Helper()
	err := json.NewDecoder(resp.Body).Decode(out)
	require.True(t, err == nil || err == io.EOF)
}
