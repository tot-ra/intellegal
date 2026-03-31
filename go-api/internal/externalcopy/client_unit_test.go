//go:build !integration

package externalcopy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestCopyDocumentRetriesAndSucceeds(t *testing.T) {
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
			return
		}
		if r.URL.Path != "/copies" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer top-secret" {
			t.Fatalf("unexpected auth header: %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"copy_id":"cp-1","status":"accepted"}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "top-secret", time.Second, 3)
	result, err := client.CopyDocument(context.Background(), CopyRequest{DocumentID: "doc-1", Filename: "contract.pdf"})
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts, got %d", attempts)
	}
	if result.Attempts != 3 {
		t.Fatalf("expected result attempts=3, got %d", result.Attempts)
	}
	if result.Body["copy_id"] != "cp-1" {
		t.Fatalf("expected parsed response body, got %#v", result.Body)
	}
}

func TestCopyDocumentReturnsNonRetriableError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid payload"))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second, 5)
	_, err := client.CopyDocument(context.Background(), CopyRequest{DocumentID: "doc-1"})
	if err == nil {
		t.Fatal("expected an error")
	}

	var callErr *CallError
	if ok := errors.As(err, &callErr); !ok {
		t.Fatalf("expected CallError, got %T", err)
	}
	if callErr.Retriable {
		t.Fatal("expected non-retriable error")
	}
	if callErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected status 400, got %d", callErr.StatusCode)
	}
	if callErr.Attempts != 1 {
		t.Fatalf("expected 1 attempt for non-retriable status, got %d", callErr.Attempts)
	}
}

func TestCopyDocumentDisabledClient(t *testing.T) {
	client := NewClient("", "", time.Second, 3)
	if client.Enabled() {
		t.Fatal("expected disabled client")
	}

	_, err := client.CopyDocument(context.Background(), CopyRequest{DocumentID: "doc-1"})
	if err == nil {
		t.Fatal("expected error for disabled client")
	}
}
