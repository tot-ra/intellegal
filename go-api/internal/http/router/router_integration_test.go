//go:build integration

package router

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	logger "github.com/Gratheon/log-lib-go"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/http/handlers"
	"legal-doc-intel/go-api/internal/logging"
)

type mockAIClient struct{}

func (mockAIClient) AnalyzeClause(_ context.Context, _ ai.AnalyzeClauseRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}
func (mockAIClient) AnalyzeLLMReview(_ context.Context, _ ai.AnalyzeLLMReviewRequest) (ai.AnalysisResult, error) {
	return ai.AnalysisResult{}, nil
}
func (mockAIClient) ContractChat(_ context.Context, _ ai.ContractChatRequest) (ai.ContractChatResult, error) {
	return ai.ContractChatResult{}, nil
}
func (mockAIClient) Extract(_ context.Context, req ai.ExtractRequest) (ai.ExtractResult, error) {
	return ai.ExtractResult{MIMEType: req.MIMEType, Text: "ok"}, nil
}
func (mockAIClient) Index(_ context.Context, req ai.IndexRequest) (ai.IndexResult, error) {
	return ai.IndexResult{DocumentID: req.DocumentID, Checksum: req.VersionChecksum, Indexed: true}, nil
}
func (mockAIClient) SearchSections(_ context.Context, _ ai.SearchSectionsRequest) (ai.SearchSectionsResult, error) {
	return ai.SearchSectionsResult{}, nil
}

func TestHealthEndpoint_ReturnsOKAndRequestID(t *testing.T) {
	// arrange
	log := logging.NewDiscard(logger.New(logger.LoggerConfig{}))
	api := handlers.NewAPI(log, mockAIClient{}, nil, nil)
	handler := New(log, api, nil, []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	w := httptest.NewRecorder()

	// act
	handler.ServeHTTP(w, req)

	// assert
	require.Equal(t, http.StatusOK, w.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	_, ok := body["timestamp"].(string)
	assert.True(t, ok)
	assert.NotEmpty(t, w.Header().Get("X-Request-ID"))
}

func TestReadinessEndpoint_ReturnsOKWhenDependencyCheckSucceeds(t *testing.T) {
	// arrange
	log := logging.NewDiscard(logger.New(logger.LoggerConfig{}))
	api := handlers.NewAPI(log, mockAIClient{}, nil, nil)
	handler := New(log, api, []handlers.DependencyProbe{
		handlers.NewDependencyProbe("postgres", func(_ context.Context) error { return nil }),
	}, []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/readiness", nil)
	w := httptest.NewRecorder()

	// act
	handler.ServeHTTP(w, req)

	// assert
	require.Equal(t, http.StatusOK, w.Code)

	var body struct {
		Status       string                       `json:"status"`
		Dependencies map[string]map[string]string `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "ready", body.Status)
	assert.Equal(t, "up", body.Dependencies["postgres"]["status"])
}

func TestReadinessEndpoint_ReturnsServiceUnavailableWhenDependencyCheckFails(t *testing.T) {
	// arrange
	log := logging.NewDiscard(logger.New(logger.LoggerConfig{}))
	api := handlers.NewAPI(log, mockAIClient{}, nil, nil)
	handler := New(
		log,
		api,
		[]handlers.DependencyProbe{
			handlers.NewDependencyProbe("postgres", func(_ context.Context) error { return context.DeadlineExceeded }),
		},
		[]string{"http://localhost:3000"},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/readiness", nil)
	w := httptest.NewRecorder()

	// act
	handler.ServeHTTP(w, req)

	// assert
	require.Equal(t, http.StatusServiceUnavailable, w.Code)

	var body struct {
		Status       string                       `json:"status"`
		Dependencies map[string]map[string]string `json:"dependencies"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &body))
	assert.Equal(t, "not_ready", body.Status)
	assert.Equal(t, "down", body.Dependencies["postgres"]["status"])
}
