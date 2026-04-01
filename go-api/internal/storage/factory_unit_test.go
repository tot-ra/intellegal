//go:build !integration

package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAdapter_ReturnsMinIOAdapter(t *testing.T) {
	// arrange

	// act
	adapter, err := NewAdapter(FactoryConfig{
		MinIOEndpoint:  "localhost:9000",
		MinIOAccessKey: "minioadmin",
		MinIOSecretKey: "minioadmin",
		MinIOBucket:    "contracts",
	})
	require.NoError(t, err)

	// assert
	if _, ok := adapter.(*MinIOAdapter); !ok {
		require.Failf(t, "expected MinIOAdapter type", "got %T", adapter)
	}
}
