//go:build !integration

package handlers

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"legal-doc-intel/go-api/internal/ai"
	"legal-doc-intel/go-api/internal/ids"
)

func TestNormalizeTags_RemovesDuplicatesAndWhitespace(t *testing.T) {
	// arrange

	// act
	tags, err := normalizeTags([]string{"  Finance  ", "finance", "", "MSA"})
	require.NoError(t, err)

	// assert
	require.Len(t, tags, 2)
	assert.Equal(t, []string{"Finance", "MSA"}, tags)
}

func TestNormalizeTags_ReturnsErrorForLongAndExcessiveTags(t *testing.T) {
	// arrange
	longTag := "abcdefghijklmnopqrstuvwxyzabcdefghijklmnopqrstuvwxyz"

	// act
	_, err := normalizeTags([]string{longTag})
	require.Error(t, err)

	input := make([]string, 21)
	for i := range input {
		input[i] = ids.NewUUID()
	}

	// assert
	_, err = normalizeTags(input)
	require.Error(t, err)
}

func TestParseTagFilters_MergesSingularAndPluralTagParams(t *testing.T) {
	// arrange

	// act
	filters, err := parseTagFilters(url.Values{
		"tag":  []string{"Finance"},
		"tags": []string{"MSA, procurement "},
	})
	require.NoError(t, err)

	// assert
	assert.Len(t, filters, 3)
}

func TestDocumentHasAnyTag_MatchesTagsCaseInsensitively(t *testing.T) {
	// arrange
	doc := document{Tags: []string{"Finance", "MSA"}}

	// act
	gotMatch := documentHasAnyTag(doc, []string{"finance"})
	gotMiss := documentHasAnyTag(doc, []string{"privacy"})

	// assert
	assert.True(t, gotMatch)
	assert.False(t, gotMiss)
}

func TestCombineExtractedText_PrefersExplicitTextAndOtherwiseCombinesPages(t *testing.T) {
	// arrange

	// act
	got := combineExtractedText(ai.ExtractResult{
		Pages: []ai.ExtractPage{
			{PageNumber: 1, Text: " First page "},
			{PageNumber: 2, Text: ""},
			{PageNumber: 3, Text: "Third page"},
		},
	})

	verbatim := combineExtractedText(ai.ExtractResult{
		Text: "  indexed text  ",
		Pages: []ai.ExtractPage{
			{PageNumber: 1, Text: "ignored"},
		},
	})

	// assert
	assert.Equal(t, "First page\n\nThird page", got)
	assert.Equal(t, "indexed text", verbatim)
}

func TestWriteCreateDocumentError_ReturnsBadRequestForTagValidationError(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	recorder := httptest.NewRecorder()
	err := errors.New("tag must be at most 50 characters")

	// act
	api.writeCreateDocumentError(recorder, err)

	// assert
	assert.Equal(t, http.StatusBadRequest, recorder.Code)

	body := decodeJSONBody(t, recorder)
	assert.Equal(t, "invalid_argument", body.Error.Code)
}

func TestWriteCreateDocumentError_ReturnsBadGatewayForPersistenceFailure(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	recorder := httptest.NewRecorder()
	err := errors.New("failed to persist document")

	// act
	api.writeCreateDocumentError(recorder, err)

	// assert
	assert.Equal(t, http.StatusBadGateway, recorder.Code)

	body := decodeJSONBody(t, recorder)
	assert.Equal(t, "storage_unavailable", body.Error.Code)
}

func TestWriteCreateDocumentError_ReturnsInternalServerErrorForUnexpectedFailure(t *testing.T) {
	// arrange
	api := NewAPI(noopLogger{}, nil, nil, nil)
	recorder := httptest.NewRecorder()
	err := errors.New("boom")

	// act
	api.writeCreateDocumentError(recorder, err)

	// assert
	assert.Equal(t, http.StatusInternalServerError, recorder.Code)

	body := decodeJSONBody(t, recorder)
	assert.Equal(t, "internal_error", body.Error.Code)
}
