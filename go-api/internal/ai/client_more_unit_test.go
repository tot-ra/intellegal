//go:build !integration

package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewClient_TrimsBaseURLAndDefaultsTimeout(t *testing.T) {
	// arrange

	// act
	client := NewClient("https://example.test/", "", 0)

	// assert
	assert.Equal(t, "https://example.test", client.baseURL)
	assert.Equal(t, 10*time.Second, client.httpClient.Timeout)
}

func TestPostJSONWithResponse_ReturnsErrorForUnexpectedStatus(t *testing.T) {
	t.Parallel()

	// arrange
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, " request failed ", http.StatusBadGateway)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	err := client.postJSONWithResponse(context.Background(), "/internal/v1/test", map[string]string{"ok": "yes"}, &struct{}{})

	// assert
	require.EqualError(t, err, "unexpected status 502: request failed")
}

func TestPostJSONWithResponse_ReturnsDecodeErrorForInvalidJSON(t *testing.T) {
	t.Parallel()

	// arrange
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		_, _ = w.Write([]byte("{"))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	err := client.postJSONWithResponse(context.Background(), "/internal/v1/test", map[string]string{"ok": "yes"}, &struct{}{})

	// assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "decode response:")
}

func TestPostJSON_ReturnsMarshalError(t *testing.T) {
	t.Parallel()

	// arrange
	client := NewClient("https://example.test", "", time.Second)

	// act
	err := client.postJSON(context.Background(), "/internal/v1/test", map[string]any{"bad": func() {}})

	// assert
	require.Error(t, err)
	assert.Contains(t, err.Error(), "marshal request:")
}

func TestContractChat_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	// arrange
	var seenPath string
	var seenBody ContractChatRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&seenBody))
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":   "job-chat-1",
			"status":   "completed",
			"job_type": "contract_chat",
			"result": map[string]any{
				"answer": "Answer",
				"citations": []map[string]any{
					{"document_id": "doc-1", "snippet_text": "citation"},
				},
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	result, err := client.ContractChat(context.Background(), ContractChatRequest{
		JobID:      "job-chat-1",
		ContractID: "contract-1",
		Messages:   []ContractChatMessage{{Role: "user", Content: "Question?"}},
		Documents:  []ContractChatDocument{{DocumentID: "doc-1", Text: "Contract text"}},
	})
	require.NoError(t, err)

	// assert
	assert.Equal(t, "/internal/v1/chat/contract", seenPath)
	assert.Equal(t, "contract-1", seenBody.ContractID)
	assert.Len(t, seenBody.Messages, 1)
	assert.Len(t, seenBody.Documents, 1)
	assert.Equal(t, "Answer", result.Answer)
	assert.Len(t, result.Citations, 1)
}

func TestExtract_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	// arrange
	var seenPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":   "job-extract-1",
			"status":   "completed",
			"job_type": "extract",
			"result": map[string]any{
				"mime_type": "application/pdf",
				"text":      "Extracted text",
				"pages": []map[string]any{
					{"page_number": 1, "text": "Extracted text", "char_count": 14, "confidence": 0.9, "source": "ocr"},
				},
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	result, err := client.Extract(context.Background(), ExtractRequest{
		JobID:      "job-extract-1",
		DocumentID: "doc-1",
		StorageURI: "s3://bucket/doc-1.pdf",
		MIMEType:   "application/pdf",
	})
	require.NoError(t, err)

	// assert
	assert.Equal(t, "/internal/v1/extract", seenPath)
	assert.Equal(t, "application/pdf", result.MIMEType)
	assert.Len(t, result.Pages, 1)
}

func TestIndex_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	// arrange
	var seenPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":   "job-index-1",
			"status":   "completed",
			"job_type": "index",
			"result": map[string]any{
				"document_id": "doc-1",
				"checksum":    "abc123",
				"chunk_count": 2,
				"indexed":     true,
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	result, err := client.Index(context.Background(), IndexRequest{
		JobID:           "job-index-1",
		DocumentID:      "doc-1",
		VersionChecksum: "abc123",
		ExtractedText:   "text",
	})
	require.NoError(t, err)

	// assert
	assert.Equal(t, "/internal/v1/index", seenPath)
	assert.Equal(t, "doc-1", result.DocumentID)
	assert.True(t, result.Indexed)
}

func TestSearchSections_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	// arrange
	var seenPath string

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":   "job-search-1",
			"status":   "completed",
			"job_type": "search_sections",
			"result": map[string]any{
				"items": []map[string]any{
					{
						"document_id":  "doc-1",
						"page_number":  3,
						"chunk_id":     "chunk-3",
						"score":        0.93,
						"snippet_text": "payment terms",
					},
				},
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	result, err := client.SearchSections(context.Background(), SearchSectionsRequest{
		JobID:       "job-search-1",
		QueryText:   "payment terms",
		DocumentIDs: []string{"doc-1"},
		Limit:       3,
		Strategy:    "semantic",
	})
	require.NoError(t, err)

	// assert
	assert.Equal(t, "/internal/v1/search/sections", seenPath)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "doc-1", result.Items[0].DocumentID)
}
