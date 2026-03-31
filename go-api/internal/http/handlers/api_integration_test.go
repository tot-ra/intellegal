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

func (f *fakeAIClient) AnalyzeCompanyName(_ context.Context, req ai.AnalyzeCompanyNameRequest) (ai.AnalysisResult, error) {
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

func TestDocumentAndCheckFlow(t *testing.T) {
	log := logging.NewDiscard(logger.New(logger.LoggerConfig{}))
	api := handlers.NewAPI(log, &fakeAIClient{}, nil, nil)
	ts := httptest.NewServer(router.New(log, api, nil, []string{"http://localhost:3000"}))
	defer ts.Close()

	docResp := postJSON(t, ts.URL+"/api/v1/documents", map[string]any{
		"filename":       "contract.pdf",
		"mime_type":      "application/pdf",
		"content_base64": "cGRm",
		"tags":           []string{"MSA", "finance"},
	})
	if docResp.StatusCode != http.StatusCreated {
		t.Fatalf("expected 201, got %d", docResp.StatusCode)
	}

	var createdDoc map[string]any
	decodeResponse(t, docResp, &createdDoc)
	docID := createdDoc["id"].(string)
	if docID == "" {
		t.Fatal("expected document id")
	}
	tags, ok := createdDoc["tags"].([]any)
	if !ok || len(tags) != 2 {
		t.Fatalf("expected 2 tags on created document, got %#v", createdDoc["tags"])
	}

	listResp := get(t, ts.URL+"/api/v1/documents")
	if listResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200, got %d", listResp.StatusCode)
	}

	checkPayload := map[string]any{
		"document_ids":         []string{docID},
		"required_clause_text": "must include payment terms",
	}

	checkResp1 := postJSONWithHeaders(t, ts.URL+"/api/v1/guidelines/clause-presence", checkPayload, map[string]string{"Idempotency-Key": "idem-key-12345"})
	if checkResp1.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202, got %d", checkResp1.StatusCode)
	}

	var accepted1 map[string]any
	decodeResponse(t, checkResp1, &accepted1)
	checkID := accepted1["check_id"].(string)

	checkResp2 := postJSONWithHeaders(t, ts.URL+"/api/v1/guidelines/clause-presence", checkPayload, map[string]string{"Idempotency-Key": "idem-key-12345"})
	if checkResp2.StatusCode != http.StatusAccepted {
		t.Fatalf("expected 202 for idempotent replay, got %d", checkResp2.StatusCode)
	}
	var accepted2 map[string]any
	decodeResponse(t, checkResp2, &accepted2)
	if accepted2["check_id"].(string) != checkID {
		t.Fatalf("expected same check id, got %q vs %q", accepted2["check_id"], checkID)
	}

	conflictResp := postJSONWithHeaders(t, ts.URL+"/api/v1/guidelines/clause-presence", map[string]any{
		"document_ids":         []string{docID},
		"required_clause_text": "different payload",
	}, map[string]string{"Idempotency-Key": "idem-key-12345"})
	if conflictResp.StatusCode != http.StatusConflict {
		t.Fatalf("expected 409 for idempotency conflict, got %d", conflictResp.StatusCode)
	}

	waitForCheckStatus(t, ts.URL, checkID, "completed")

	resultsResp := get(t, ts.URL+"/api/v1/guidelines/"+checkID+"/results")
	if resultsResp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 results, got %d", resultsResp.StatusCode)
	}

	var results struct {
		Items []map[string]any `json:"items"`
	}
	decodeResponse(t, resultsResp, &results)
	if len(results.Items) != 1 {
		t.Fatalf("expected one result item, got %d", len(results.Items))
	}
}

func waitForCheckStatus(t *testing.T, baseURL, checkID, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		resp := get(t, baseURL+"/api/v1/guidelines/"+checkID)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("unexpected status for check: %d", resp.StatusCode)
		}
		var body map[string]any
		decodeResponse(t, resp, &body)
		if body["status"] == want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("check %s did not reach status %s", checkID, want)
}

func postJSON(t *testing.T, url string, payload any) *http.Response {
	t.Helper()
	return postJSONWithHeaders(t, url, payload, nil)
}

func postJSONWithHeaders(t *testing.T, url string, payload any, headers map[string]string) *http.Response {
	t.Helper()
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func get(t *testing.T, url string) *http.Response {
	t.Helper()
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	return resp
}

func decodeResponse(t *testing.T, resp *http.Response, out any) {
	t.Helper()
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		t.Fatal(err)
	}
}
