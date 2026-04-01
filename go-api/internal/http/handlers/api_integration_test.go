//go:build integration

package handlers_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	logger "github.com/Gratheon/log-lib-go"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/http/handlers"
	"legal-doc-intel/go-api/internal/http/router"
	"legal-doc-intel/go-api/internal/logging"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type fakeAIClient struct {
	fail bool
}

func (f *fakeAIClient) AnalyzeClause(_ context.Context, req ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	if f.fail {
		return ai.AnalysisResult{}, context.DeadlineExceeded
	}
	items := make([]ai.AnalysisResultItem, 0, len(req.DocumentIDs))
	for _, documentID := range req.DocumentIDs {
		items = append(items, ai.AnalysisResultItem{
			DocumentID: documentID,
			Outcome:    "review",
			Confidence: 0.5,
			Summary:    "analysis placeholder",
		})
	}
	return ai.AnalysisResult{Items: items}, nil
}

func (f *fakeAIClient) AnalyzeLLMReview(_ context.Context, req ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	items := make([]ai.AnalysisResultItem, 0, len(req.DocumentIDs))
	for _, docID := range req.DocumentIDs {
		items = append(items, ai.AnalysisResultItem{
			DocumentID: docID,
			Outcome:    "review",
			Confidence: 0.75,
			Summary:    "LLM review required.",
		})
	}
	return ai.AnalysisResult{Items: items}, nil
}

func (f *fakeAIClient) ContractChat(_ context.Context, _ ai.ContractChatRequest) (ai.ContractChatResult, error) {
	if f.fail {
		return ai.ContractChatResult{}, context.DeadlineExceeded
	}
	return ai.ContractChatResult{
		Answer: "Chat answer placeholder.",
		Citations: []ai.ContractChatCitation{
			{
				DocumentID:  "doc-1",
				SnippetText: "sample text",
				Reason:      "Supports the answer.",
			},
		},
	}, nil
}

func (f *fakeAIClient) Extract(_ context.Context, req ai.ExtractRequest) (ai.ExtractResult, error) {
	if f.fail {
		return ai.ExtractResult{}, context.DeadlineExceeded
	}
	return ai.ExtractResult{
		MIMEType: req.MIMEType,
		Text:     "sample text",
		Pages: []ai.ExtractPage{
			{PageNumber: 1, Text: "sample text"},
		},
	}, nil
}

func (f *fakeAIClient) Index(_ context.Context, req ai.IndexRequest) (ai.IndexResult, error) {
	if f.fail {
		return ai.IndexResult{}, context.DeadlineExceeded
	}
	return ai.IndexResult{
		DocumentID: req.DocumentID,
		Checksum:   req.VersionChecksum,
		ChunkCount: 1,
		Indexed:    true,
	}, nil
}

func (f *fakeAIClient) SearchSections(_ context.Context, _ ai.SearchSectionsRequest) (ai.SearchSectionsResult, error) {
	if f.fail {
		return ai.SearchSectionsResult{}, context.DeadlineExceeded
	}
	return ai.SearchSectionsResult{
		Items: []ai.SearchSectionsResultItem{},
	}, nil
}

func TestDocumentAndCheckFlow_CreatesDocumentsAndReturnsCompletedResults(t *testing.T) {
	// arrange
	log := logging.NewDiscard(logger.New(logger.LoggerConfig{}))
	api := handlers.NewAPI(log, &fakeAIClient{}, nil, nil)
	api.EnableInMemoryReadersForTesting()
	ts := httptest.NewServer(router.New(log, api, nil, []string{"http://localhost:3000"}))
	defer ts.Close()

	// act
	docResp := postJSON(t, ts.URL+"/api/v1/documents", map[string]any{
		"filename":       "contract.pdf",
		"mime_type":      "application/pdf",
		"content_base64": "cGRm",
		"tags":           []string{"MSA", "finance"},
	})
	require.Equal(t, http.StatusCreated, docResp.StatusCode)

	// assert
	var createdDoc map[string]any
	decodeResponse(t, docResp, &createdDoc)
	docID := createdDoc["id"].(string)
	require.NotEmpty(t, docID)
	tags, ok := createdDoc["tags"].([]any)
	require.True(t, ok)
	assert.Len(t, tags, 2)

	listResp := get(t, ts.URL+"/api/v1/documents")
	require.Equal(t, http.StatusOK, listResp.StatusCode)

	checkPayload := map[string]any{
		"document_ids":         []string{docID},
		"required_clause_text": "must include payment terms",
	}

	checkResp1 := postJSONWithHeaders(t, ts.URL+"/api/v1/guidelines/clause-presence", checkPayload, map[string]string{"Idempotency-Key": "idem-key-12345"})
	require.Equal(t, http.StatusAccepted, checkResp1.StatusCode)

	var accepted1 map[string]any
	decodeResponse(t, checkResp1, &accepted1)
	checkID := accepted1["check_id"].(string)

	checkResp2 := postJSONWithHeaders(t, ts.URL+"/api/v1/guidelines/clause-presence", checkPayload, map[string]string{"Idempotency-Key": "idem-key-12345"})
	require.Equal(t, http.StatusAccepted, checkResp2.StatusCode)
	var accepted2 map[string]any
	decodeResponse(t, checkResp2, &accepted2)
	assert.Equal(t, checkID, accepted2["check_id"].(string))

	conflictResp := postJSONWithHeaders(t, ts.URL+"/api/v1/guidelines/clause-presence", map[string]any{
		"document_ids":         []string{docID},
		"required_clause_text": "different payload",
	}, map[string]string{"Idempotency-Key": "idem-key-12345"})
	require.Equal(t, http.StatusConflict, conflictResp.StatusCode)

	waitForCheckStatus(t, ts.URL, checkID, "completed")

	resultsResp := get(t, ts.URL+"/api/v1/guidelines/"+checkID+"/results")
	require.Equal(t, http.StatusOK, resultsResp.StatusCode)

	var results struct {
		Items []map[string]any `json:"items"`
	}
	decodeResponse(t, resultsResp, &results)
	assert.Len(t, results.Items, 1)
}

func TestContractLifecycle_ManagesFilesAndOrderingThroughRouter(t *testing.T) {
	// arrange
	log := logging.NewDiscard(logger.New(logger.LoggerConfig{}))
	api := handlers.NewAPI(log, &fakeAIClient{}, nil, nil)
	api.EnableInMemoryReadersForTesting()
	ts := httptest.NewServer(router.New(log, api, nil, []string{"http://localhost:3000"}))
	defer ts.Close()

	// act
	createResp := postJSON(t, ts.URL+"/api/v1/contracts", map[string]any{
		"name":        "MSA 2026",
		"source_type": "api",
		"tags":        []string{"Finance", "MSA"},
	})
	require.Equal(t, http.StatusCreated, createResp.StatusCode)

	var createdContract struct {
		ID         string   `json:"id"`
		Name       string   `json:"name"`
		SourceType string   `json:"source_type"`
		Tags       []string `json:"tags"`
		FileCount  int      `json:"file_count"`
	}
	decodeResponse(t, createResp, &createdContract)
	require.NotEmpty(t, createdContract.ID)
	assert.Equal(t, "MSA 2026", createdContract.Name)
	assert.Equal(t, "api", createdContract.SourceType)
	assert.Len(t, createdContract.Tags, 2)
	assert.Zero(t, createdContract.FileCount)

	firstFileResp := postJSON(t, ts.URL+"/api/v1/contracts/"+createdContract.ID+"/files", map[string]any{
		"filename":       "main.pdf",
		"mime_type":      "application/pdf",
		"content_base64": "cGRm",
	})
	require.Equal(t, http.StatusCreated, firstFileResp.StatusCode)
	var firstFile struct {
		ID         string `json:"id"`
		ContractID string `json:"contract_id"`
		Status     string `json:"status"`
	}
	decodeResponse(t, firstFileResp, &firstFile)
	require.NotEmpty(t, firstFile.ID)
	assert.Equal(t, createdContract.ID, firstFile.ContractID)
	assert.Equal(t, "indexed", firstFile.Status)

	secondFileResp := postJSON(t, ts.URL+"/api/v1/contracts/"+createdContract.ID+"/files", map[string]any{
		"filename":       "appendix.png",
		"mime_type":      "image/png",
		"content_base64": "cG5n",
	})
	require.Equal(t, http.StatusCreated, secondFileResp.StatusCode)
	var secondFile struct {
		ID string `json:"id"`
	}
	decodeResponse(t, secondFileResp, &secondFile)
	require.NotEmpty(t, secondFile.ID)

	// assert
	listResp := get(t, ts.URL+"/api/v1/contracts?limit=10&offset=0")
	require.Equal(t, http.StatusOK, listResp.StatusCode)
	var listed struct {
		Items []struct {
			ID        string `json:"id"`
			FileCount int    `json:"file_count"`
		} `json:"items"`
		Total int `json:"total"`
	}
	decodeResponse(t, listResp, &listed)
	assert.Equal(t, 1, listed.Total)
	require.Len(t, listed.Items, 1)
	assert.Equal(t, createdContract.ID, listed.Items[0].ID)
	assert.Equal(t, 2, listed.Items[0].FileCount)

	getResp := get(t, ts.URL+"/api/v1/contracts/"+createdContract.ID)
	require.Equal(t, http.StatusOK, getResp.StatusCode)
	var fetched struct {
		ID        string `json:"id"`
		FileCount int    `json:"file_count"`
		Files     []struct {
			ID       string `json:"id"`
			Filename string `json:"filename"`
			Status   string `json:"status"`
		} `json:"files"`
	}
	decodeResponse(t, getResp, &fetched)
	assert.Equal(t, createdContract.ID, fetched.ID)
	assert.Equal(t, 2, fetched.FileCount)
	require.Len(t, fetched.Files, 2)
	assert.Equal(t, firstFile.ID, fetched.Files[0].ID)
	assert.Equal(t, secondFile.ID, fetched.Files[1].ID)

	reorderResp := requestJSON(t, http.MethodPatch, ts.URL+"/api/v1/contracts/"+createdContract.ID+"/files/order", map[string]any{
		"file_ids": []string{secondFile.ID, firstFile.ID},
	}, nil)
	require.Equal(t, http.StatusOK, reorderResp.StatusCode)
	var reordered struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	decodeResponse(t, reorderResp, &reordered)
	require.Len(t, reordered.Files, 2)
	assert.Equal(t, secondFile.ID, reordered.Files[0].ID)
	assert.Equal(t, firstFile.ID, reordered.Files[1].ID)

	deleteResp := requestJSON(t, http.MethodDelete, ts.URL+"/api/v1/contracts/"+createdContract.ID, nil, nil)
	require.Equal(t, http.StatusNoContent, deleteResp.StatusCode)
	_ = deleteResp.Body.Close()

	missingResp := get(t, ts.URL+"/api/v1/contracts/"+createdContract.ID)
	assert.Equal(t, http.StatusNotFound, missingResp.StatusCode)
}

func waitForCheckStatus(t *testing.T, baseURL, checkID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp := get(t, baseURL+"/api/v1/guidelines/"+checkID)
		require.Equal(t, http.StatusOK, resp.StatusCode)
		var body map[string]any
		decodeResponse(t, resp, &body)
		if body["status"] == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	require.FailNowf(t, "check did not reach desired status", "check %s did not reach status %s", checkID, want)
}

func postJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	return requestJSON(t, http.MethodPost, url, payload, nil)
}

func postJSONWithHeaders(t *testing.T, url string, payload any, headers map[string]string) *http.Response {
	t.Helper()
	return requestJSON(t, http.MethodPost, url, payload, headers)
}

func requestJSON(t *testing.T, method, url string, payload any, headers map[string]string) *http.Response {
	t.Helper()
	var body *bytes.Reader
	if payload == nil {
		body = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(payload)
		require.NoError(t, err)
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, url, body)
	require.NoError(t, err)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	return resp
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	require.NoError(t, err)
	return resp
}

func decodeResponse(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	require.NoError(t, json.NewDecoder(resp.Body).Decode(out))
}
