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

func TestAnalyzeClause_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	// arrange
	var seenPath string
	var seenAuth string
	var seenInternalToken string
	var seenBody AnalyzeClauseRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		seenAuth = r.Header.Get("Authorization")
		seenInternalToken = r.Header.Get("X-Internal-Service-Token")
		require.NoError(t, json.NewDecoder(r.Body).Decode(&seenBody))
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":   "job-1",
			"status":   "completed",
			"job_type": "analyze_clause",
			"result": map[string]any{
				"items": []map[string]any{
					{
						"document_id": "doc-1",
						"outcome":     "match",
						"confidence":  0.9,
					},
				},
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "secret-token", time.Second)

	// act
	result, err := client.AnalyzeClause(context.Background(), AnalyzeClauseRequest{
		JobID:              "job-1",
		RequestID:          "req-1",
		CheckID:            "check-1",
		DocumentIDs:        []string{"doc-1"},
		RequiredClauseText: "must include payment terms",
		ContextHint:        "billing",
	})
	require.NoError(t, err)

	// assert
	assert.Equal(t, "/internal/v1/analyze/clause", seenPath)
	assert.Equal(t, "Bearer secret-token", seenAuth)
	assert.Equal(t, "secret-token", seenInternalToken)
	assert.Equal(t, "check-1", seenBody.CheckID)
	assert.NotEmpty(t, seenBody.RequiredClauseText)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "doc-1", result.Items[0].DocumentID)
}

func TestAnalyzeLLMReview_PostsExpectedRequest(t *testing.T) {
	t.Parallel()

	// arrange
	var seenPath string
	var seenBody AnalyzeLLMReviewRequest

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenPath = r.URL.Path
		require.NoError(t, json.NewDecoder(r.Body).Decode(&seenBody))
		w.WriteHeader(http.StatusAccepted)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id":   "job-llm-1",
			"status":   "completed",
			"job_type": "analyze_llm_review",
			"result": map[string]any{
				"items": []map[string]any{
					{
						"document_id": "doc-1",
						"outcome":     "review",
						"confidence":  0.77,
					},
				},
			},
		})
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second)

	// act
	result, err := client.AnalyzeLLMReview(context.Background(), AnalyzeLLMReviewRequest{
		JobID:        "job-llm-1",
		CheckID:      "check-llm-1",
		DocumentIDs:  []string{"doc-1"},
		Instructions: "Review the entire contract for termination for convenience.",
		Documents: []AnalyzeDocument{
			{DocumentID: "doc-1", Filename: "contract.pdf", Text: "Contract text"},
		},
	})
	require.NoError(t, err)

	// assert
	assert.Equal(t, "/internal/v1/analyze/llm-review", seenPath)
	assert.NotEmpty(t, seenBody.Instructions)
	require.Len(t, seenBody.Documents, 1)
	assert.NotEmpty(t, seenBody.Documents[0].Text)
	require.Len(t, result.Items, 1)
	assert.Equal(t, "review", result.Items[0].Outcome)
}
