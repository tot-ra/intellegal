//go:build !integration

package handlers

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHashPayload_IsDeterministic(t *testing.T) {
	// arrange
	payload := map[string]any{
		"required_clause_text": "Payment terms",
		"context_hint":         "MSA",
	}
	documentIDs := []string{
		"550e8400-e29b-41d4-a716-446655440000",
		"550e8400-e29b-41d4-a716-446655440001",
	}

	// act
	left, err := hashPayload(payload, documentIDs)
	require.NoError(t, err)
	right, err := hashPayload(payload, documentIDs)
	require.NoError(t, err)

	// assert
	assert.Equal(t, left, right)
}

func TestHashPayload_ReturnsErrorForUnmarshalablePayload(t *testing.T) {
	// arrange

	// act
	_, err := hashPayload(map[string]any{"bad": func() {}}, nil)

	// assert
	require.Error(t, err)
}
