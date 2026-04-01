//go:build !integration

package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/db"

	"github.com/go-chi/chi/v5"
)

func TestDecodeJSON_RejectsUnknownFields(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/checks/clause-presence", bytes.NewReader([]byte(`{
		"required_clause_text":"payment terms",
		"unexpected":"value"
	}`)))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()

	// act
	api.CreateClauseCheck(rec, req)

	// assert
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeJSONBody(t, rec)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestExtensionForFilename_UsesFilenameThenMIMEFallback(t *testing.T) {
	// arrange

	// act
	gotPDF := extensionForFilename("contract.PDF", "image/png")
	gotPNG := extensionForFilename("contract", "image/png")
	gotJPG := extensionForFilename("contract", "image/jpeg")

	// assert
	assert.Equal(t, ".pdf", gotPDF)
	assert.Equal(t, ".png", gotPNG)
	assert.Equal(t, ".jpg", gotJPG)
}

func TestPathParam_UsesChiParamBeforePathValue(t *testing.T) {
	// arrange
	req := httptest.NewRequest(http.MethodGet, "/ignored", nil)
	req.SetPathValue("document_id", " path-value ")

	routeCtx := chiRouteContextWithParam("document_id", " chi-value ")
	req = req.WithContext(routeCtx)

	// act
	got := pathParam(req, "document_id")

	// assert
	assert.Equal(t, "chi-value", got)
}

func chiRouteContextWithParam(key, value string) context.Context {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return context.WithValue(context.Background(), chi.RouteCtxKey, routeCtx)
}

func TestSearchCandidateLimit_OverfetchesForContractMode(t *testing.T) {
	// arrange

	// act
	gotSections := searchCandidateLimit(4, "sections")
	gotContractsMin := searchCandidateLimit(4, "contracts")
	gotContractsMax := searchCandidateLimit(20, "contracts")

	// assert
	assert.Equal(t, 4, gotSections)
	assert.Equal(t, 20, gotContractsMin)
	assert.Equal(t, 50, gotContractsMax)
}

func TestCollapseContractSearchResults_KeepsBestItemPerGroup(t *testing.T) {
	// arrange
	items := collapseContractSearchResults([]contractSearchResultItem{
		{DocumentID: "doc-1", ContractID: "contract-1", Score: 0.61, PageNumber: 5},
		{DocumentID: "doc-2", ContractID: "contract-1", Score: 0.91, PageNumber: 2},
		{DocumentID: "doc-3", ContractID: "", Score: 0.88, PageNumber: 1},
		{DocumentID: "doc-4", ContractID: "", Score: 0.77, PageNumber: 1},
	}, 3)

	// assert
	require.Len(t, items, 3)
	assert.Equal(t, "doc-2", items[0].DocumentID)
	assert.Equal(t, "doc-3", items[1].DocumentID)
}

func TestMapAnalysisItems_FallsBackForMissingDocuments(t *testing.T) {
	// arrange
	items := mapAnalysisItems(
		[]string{"doc-1", "doc-2"},
		[]ai.AnalysisResultItem{
			{
				DocumentID: "doc-1",
				Outcome:    "match",
				Confidence: 0.94,
				Summary:    "Found it",
				Evidence: []ai.AnalysisEvidenceSnippet{
					{SnippetText: "payment clause", PageNumber: 2, ChunkID: "chunk-1", Score: 0.88},
				},
			},
		},
		"fallback",
	)

	// assert
	require.Len(t, items, 2)
	assert.Equal(t, "match", items[0].Outcome)
	assert.Len(t, items[0].Evidence, 1)
	assert.Equal(t, "review", items[1].Outcome)
	assert.Equal(t, "fallback", items[1].Summary)
}

func TestHandleCreateCheckError_MapsExpectedStatuses(t *testing.T) {
	// arrange
	tests := []struct {
		name string
		err  error
		code int
		key  string
	}{
		{name: "db not configured", err: db.ErrNotConfigured, code: http.StatusServiceUnavailable, key: "service_unavailable"},
		{name: "idempotency conflict", err: errIdempotencyConflict, code: http.StatusConflict, key: "idempotency_conflict"},
		{name: "document missing", err: errors.New("document not found"), code: http.StatusBadRequest, key: "invalid_argument"},
		{name: "missing scope", err: errors.New("at least one document is required"), code: http.StatusUnprocessableEntity, key: "invalid_scope"},
		{name: "unexpected", err: errors.New("boom"), code: http.StatusInternalServerError, key: "internal_error"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// arrange
			rec := httptest.NewRecorder()

			// act
			handleCreateCheckError(rec, tc.err)

			// assert
			assert.Equal(t, tc.code, rec.Code)
			body := decodeJSONBody(t, rec)
			assert.Equal(t, tc.key, body.Error.Code)
		})
	}
}

func TestCreateLLMReviewCheck_ReturnsBadRequestForShortInstructions(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	rec := performJSONRequest(t, http.MethodPost, "/api/v1/checks/llm-review", map[string]any{
		"instructions": "abc",
	}, api.CreateLLMReviewCheck)

	// assert
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeJSONBody(t, rec)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestGetCheck_ReturnsStatusPayloadForCompletedCheck(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	checkID := "00000000-0000-4000-8000-000000000141"
	finishedAt := time.Now().UTC()
	api.checks[checkID] = checkRun{
		CheckID:       checkID,
		Status:        checkStatusCompleted,
		CheckType:     checkTypeClause,
		RequestedAt:   finishedAt.Add(-time.Minute),
		FinishedAt:    &finishedAt,
		FailureReason: "ignored once completed",
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/checks/"+checkID, nil)
	req.SetPathValue("check_id", checkID)
	rec := httptest.NewRecorder()

	// act
	api.GetCheck(rec, req)

	// assert
	assert.Equal(t, http.StatusOK, rec.Code)
	var body checkRunResponse
	decodeJSONBodyInto(t, rec, &body)
	assert.Equal(t, checkID, body.CheckID)
	assert.Equal(t, checkStatusCompleted, body.Status)
	assert.NotNil(t, body.FinishedAt)
}

func TestDeleteChecks_ReturnsBadRequestForEmptyList(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	rec := performJSONRequest(t, http.MethodDelete, "/api/v1/checks", map[string]any{
		"check_ids": []string{},
	}, api.DeleteChecks)

	// assert
	assert.Equal(t, http.StatusBadRequest, rec.Code)
	body := decodeJSONBody(t, rec)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestSearchContracts_ReturnsEmptyWhenNoDocumentsResolved(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	// act
	rec := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/search", map[string]any{
		"query_text": "payment terms",
	}, api.SearchContracts)

	// assert
	assert.Equal(t, http.StatusOK, rec.Code)
	var body contractSearchResponse
	decodeJSONBodyInto(t, rec, &body)
	assert.Empty(t, body.Items)
}

func TestContractChatDocuments_ReturnsExpectedValidationErrors(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	// act
	_, err := api.contractChatDocuments("contract-1")

	// assert
	assert.ErrorIs(t, err, db.ErrNotConfigured)

	useInMemoryReaders(api)

	_, err = api.contractChatDocuments("missing")
	require.EqualError(t, err, "contract not found")

	contractID := "00000000-0000-4000-8000-000000000101"
	now := time.Now().UTC()
	api.contracts[contractID] = contract{ID: contractID, FileIDs: nil, CreatedAt: now, UpdatedAt: now}

	_, err = api.contractChatDocuments(contractID)
	require.EqualError(t, err, "no contract files")

	documentID := "00000000-0000-4000-8000-000000000102"
	api.contracts[contractID] = contract{ID: contractID, FileIDs: []string{documentID}, CreatedAt: now, UpdatedAt: now}
	api.documents[documentID] = document{ID: documentID, Filename: "empty.pdf", ExtractedText: "   "}

	_, err = api.contractChatDocuments(contractID)
	require.EqualError(t, err, "no extracted text")
}

func TestContractChatDocuments_TrimsAndFiltersDocuments(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	useInMemoryReaders(api)

	contractID := "00000000-0000-4000-8000-000000000111"
	firstDocumentID := "00000000-0000-4000-8000-000000000112"
	secondDocumentID := "00000000-0000-4000-8000-000000000113"
	now := time.Now().UTC()

	api.contracts[contractID] = contract{
		ID:        contractID,
		FileIDs:   []string{firstDocumentID, secondDocumentID},
		CreatedAt: now,
		UpdatedAt: now,
	}
	api.documents[firstDocumentID] = document{ID: firstDocumentID, Filename: "alpha.pdf", ExtractedText: "  Alpha text  "}
	api.documents[secondDocumentID] = document{ID: secondDocumentID, Filename: "blank.pdf", ExtractedText: ""}

	// act
	documents, err := api.contractChatDocuments(contractID)
	require.NoError(t, err)

	// assert
	require.Len(t, documents, 1)
	assert.Equal(t, "Alpha text", documents[0].Text)
}

func TestChatContract_ReturnsBadGatewayWhenAIClientFails(t *testing.T) {
	// arrange
	aiClient := &capturingAIClient{chatErr: errors.New("upstream down")}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)

	contractID := "00000000-0000-4000-8000-000000000121"
	documentID := "00000000-0000-4000-8000-000000000122"
	now := time.Now().UTC()
	api.contracts[contractID] = contract{ID: contractID, FileIDs: []string{documentID}, CreatedAt: now, UpdatedAt: now}
	api.documents[documentID] = document{ID: documentID, Filename: "alpha.pdf", ExtractedText: "Some text"}

	// act
	rec := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/"+contractID+"/chat", map[string]any{
		"messages": []map[string]any{{"role": "user", "content": "Question?"}},
	}, func(w http.ResponseWriter, r *http.Request) {
		r.SetPathValue("contract_id", contractID)
		api.ChatContract(w, r)
	})

	// assert
	assert.Equal(t, http.StatusBadGateway, rec.Code)
	body := decodeJSONBody(t, rec)
	assert.Equal(t, "contract_chat_unavailable", body.Error.Code)
}

func TestChatContract_ValidatesMessages(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)

	tests := []struct {
		name    string
		payload map[string]any
	}{
		{name: "missing messages", payload: map[string]any{"messages": []map[string]any{}}},
		{name: "invalid role", payload: map[string]any{"messages": []map[string]any{{"role": "system", "content": "Question?"}}}},
		{name: "empty content", payload: map[string]any{"messages": []map[string]any{{"role": "user", "content": "   "}}}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// act
			rec := performJSONRequest(t, http.MethodPost, "/api/v1/contracts/not-a-uuid/chat", tc.payload, func(w http.ResponseWriter, r *http.Request) {
				r.SetPathValue("contract_id", "00000000-0000-4000-8000-000000000151")
				api.ChatContract(w, r)
			})

			// assert
			assert.Equal(t, http.StatusBadRequest, rec.Code)
			body := decodeJSONBody(t, rec)
			assert.Equal(t, "invalid_argument", body.Error.Code)
		})
	}
}

func TestChatContract_FiltersBlankCitationsAndTrimsAnswer(t *testing.T) {
	// arrange
	aiClient := &capturingAIClient{}
	api := NewAPI(noopLogger{}, aiClient, nil, nil)
	useInMemoryReaders(api)

	contractID := "00000000-0000-4000-8000-000000000131"
	documentID := "00000000-0000-4000-8000-000000000132"
	now := time.Now().UTC()
	api.contracts[contractID] = contract{ID: contractID, FileIDs: []string{documentID}, CreatedAt: now, UpdatedAt: now}
	api.documents[documentID] = document{ID: documentID, Filename: "alpha.pdf", ExtractedText: "Some text"}
	aiClient.chatErr = nil

	rec := httptest.NewRecorder()
	reqBody, _ := json.Marshal(contractChatRequest{
		Messages: []contractChatMessageRequest{{Role: "user", Content: "Question?"}},
	})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/contracts/"+contractID+"/chat", bytes.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	req.SetPathValue("contract_id", contractID)

	aiClient.chatErr = nil
	aiClient.chatReq = nil
	aiClient.chatErr = nil

	original := api.ai
	api.ai = contractChatFilteringStub{}
	defer func() { api.ai = original }()

	// act
	api.ChatContract(rec, req)

	// assert
	assert.Equal(t, http.StatusOK, rec.Code)

	var body contractChatResponse
	decodeJSONBodyInto(t, rec, &body)
	assert.Equal(t, "Trim me", body.Answer)
	require.Len(t, body.Citations, 1)
	assert.Equal(t, documentID, body.Citations[0].DocumentID)
}

type contractChatFilteringStub struct{}

func (contractChatFilteringStub) AnalyzeClause(_ context.Context, _ ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (contractChatFilteringStub) AnalyzeLLMReview(_ context.Context, _ ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}

func (contractChatFilteringStub) ContractChat(_ context.Context, req ai.ContractChatRequest) (ai.ContractChatResult, error) {
	return ai.ContractChatResult{
		Answer: "  Trim me  ",
		Citations: []ai.ContractChatCitation{
			{DocumentID: req.Documents[0].DocumentID, SnippetText: "  useful snippet  ", Reason: "  because  "},
			{DocumentID: "", SnippetText: "skip"},
			{DocumentID: req.Documents[0].DocumentID, SnippetText: "   "},
		},
	}, nil
}

func (contractChatFilteringStub) Extract(_ context.Context, _ ai.ExtractRequest) (ai.ExtractResult, error) {
	return ai.ExtractResult{}, nil
}

func (contractChatFilteringStub) Index(_ context.Context, _ ai.IndexRequest) (ai.IndexResult, error) {
	return ai.IndexResult{}, nil
}

func (contractChatFilteringStub) SearchSections(_ context.Context, _ ai.SearchSectionsRequest) (ai.SearchSectionsResult, error) {
	return ai.SearchSectionsResult{}, nil
}
