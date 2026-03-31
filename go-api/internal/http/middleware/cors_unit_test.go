//go:build !integration

package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCORSAddsHeadersForAllowedOrigin(t *testing.T) {
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}), []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/documents", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:3000" {
		t.Fatalf("expected Access-Control-Allow-Origin header to match origin, got %q", got)
	}
}

func TestCORSPreflightForAllowedOrigin(t *testing.T) {
	called := false
	handler := CORS(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		called = true
		w.WriteHeader(http.StatusOK)
	}), []string{"http://localhost:3000"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/documents", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	if called {
		t.Fatal("expected preflight request to short-circuit middleware")
	}
	if w.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for allowed preflight, got %d", w.Code)
	}
}
