//go:build !integration

package externalcopy

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCopyDocument_RetriesAndEventuallySucceeds(t *testing.T) {
	// arrange
	attempts := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts < 3 {
			w.WriteHeader(http.StatusBadGateway)
			_, _ = w.Write([]byte(`{"error":"upstream unavailable"}`))
			return
		}
		assert.Equal(t, "/copies", r.URL.Path)
		assert.Equal(t, "Bearer top-secret", r.Header.Get("Authorization"))
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"copy_id":"cp-1","status":"accepted"}`))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "top-secret", time.Second, 3)

	// act
	result, err := client.CopyDocument(context.Background(), CopyRequest{DocumentID: "doc-1", Filename: "contract.pdf"})
	require.NoError(t, err)

	// assert
	assert.Equal(t, 3, attempts)
	assert.Equal(t, 3, result.Attempts)
	assert.Equal(t, "cp-1", result.Body["copy_id"])
}

func TestCopyDocument_ReturnsNonRetriableErrorForBadRequest(t *testing.T) {
	// arrange
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("invalid payload"))
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "", time.Second, 5)

	// act
	_, err := client.CopyDocument(context.Background(), CopyRequest{DocumentID: "doc-1"})
	require.Error(t, err)

	// assert
	var callErr *CallError
	require.True(t, errors.As(err, &callErr))
	assert.False(t, callErr.Retriable)
	assert.Equal(t, http.StatusBadRequest, callErr.StatusCode)
	assert.Equal(t, 1, callErr.Attempts)
}

func TestCopyDocument_ReturnsErrorWhenClientIsDisabled(t *testing.T) {
	// arrange
	client := NewClient("", "", time.Second, 3)

	// act
	enabled := client.Enabled()
	_, err := client.CopyDocument(context.Background(), CopyRequest{DocumentID: "doc-1"})

	// assert
	assert.False(t, enabled)
	require.Error(t, err)
}
