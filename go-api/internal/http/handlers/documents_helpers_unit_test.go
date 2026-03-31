//go:build !integration

package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"legal-doc-intel/go-api/internal/ai"
)

func TestNormalizeTags(t *testing.T) {
	tags, err := normalizeTags([]string{"  Finance  ", "finance", "", "MSA"})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(tags) != 2 {
		t.Fatalf("expected 2 tags, got %d", len(tags))
	}
	if tags[0] != "Finance" || tags[1] != "MSA" {
		t.Fatalf("unexpected tags: %#v", tags)
	}
}

func TestNormalizeTagsRejectsLongAndTooManyTags(t *testing.T) {
	longTag := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"
	if _, err := normalizeTags([]string{longTag}); err == nil {
		t.Fatal("expected tag length validation error")
	}

	input := make([]string, 21)
	for i := range input {
		input[i] = newUUID()
	}
	if _, err := normalizeTags(input); err == nil {
		t.Fatal("expected max tag validation error")
	}
}

func TestParseTagFiltersMergesTagParams(t *testing.T) {
	filters, err := parseTagFilters(url.Values{
		"tag":  []string{"Finance"},
		"tags": []string{"MSA, procurement "},
	})
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if len(filters) != 3 {
		t.Fatalf("expected 3 filters, got %d", len(filters))
	}
}

func TestDocumentHasAnyTagIsCaseInsensitive(t *testing.T) {
	doc := document{Tags: []string{"Finance", "MSA"}}
	if !documentHasAnyTag(doc, []string{"finance"}) {
		t.Fatal("expected document to match lower-cased filter")
	}
	if documentHasAnyTag(doc, []string{"privacy"}) {
		t.Fatal("expected non-matching filter to return false")
	}
}

func TestCombineExtractedText(t *testing.T) {
	got := combineExtractedText(ai.ExtractResult{
		Pages: []ai.ExtractPage{
			{PageNumber: 1, Text: " First page "},
			{PageNumber: 2, Text: ""},
			{PageNumber: 3, Text: "Third page"},
		},
	})
	if got != "First page\n\nThird page" {
		t.Fatalf("unexpected combined text: %q", got)
	}

	verbatim := combineExtractedText(ai.ExtractResult{
		Text: "  indexed text  ",
		Pages: []ai.ExtractPage{
			{PageNumber: 1, Text: "ignored"},
		},
	})
	if verbatim != "indexed text" {
		t.Fatalf("expected explicit extracted text to win, got %q", verbatim)
	}
}

func TestWriteCreateDocumentErrorMapsResponses(t *testing.T) {
	api := NewAPI(noopLogger{}, nil, nil, nil)
	testCases := []struct {
		name   string
		err    error
		status int
		code   string
	}{
		{name: "tag validation", err: errors.New("tag must be at most 50 characters"), status: http.StatusBadRequest, code: "invalid_argument"},
		{name: "persist", err: errors.New("failed to persist document"), status: http.StatusBadGateway, code: "storage_unavailable"},
		{name: "unexpected", err: errors.New("boom"), status: http.StatusInternalServerError, code: "internal_error"},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			api.writeCreateDocumentError(w, tc.err)
			if w.Code != tc.status {
				t.Fatalf("expected status %d, got %d", tc.status, w.Code)
			}

			body := decodeJSONBody(t, w)
			if body.Error.Code != tc.code {
				t.Fatalf("expected error code %q, got %q", tc.code, body.Error.Code)
			}
		})
	}
}
