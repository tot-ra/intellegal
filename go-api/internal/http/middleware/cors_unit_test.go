//go:build !integration

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestCORS_AddsHeadersForAllowedOrigin(t *testing.T) {
	// arrange
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	// act
	handler.ServeHTTP(w, req)

	// assert
	assert.Equal(t, "http://localhost:3000", w.Header().Get("Access-Control-Allow-Origin"))
}

func TestCORS_ShortCircuitsAllowedPreflightRequests(t *testing.T) {
	// arrange
	called := false
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/documents", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()

	// act
	handler.ServeHTTP(w, req)

	// assert
	assert.False(t, called)
	assert.Equal(t, http.StatusNoContent, w.Code)
}
