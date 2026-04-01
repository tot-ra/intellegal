//go:build !integration

package storage

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNewAdapter_PropagatesValidationErrors(t *testing.T) {
	// arrange

	// act
	_, err := NewAdapter(FactoryConfig{
		MinIOAccessKey: "minioadmin",
		MinIOSecretKey: "minioadmin",
		MinIOBucket:    "contracts",
	})

	// assert
	require.EqualError(t, err, "minio endpoint is empty")
}

func TestMinIOAdapterHealthCheck_ReturnsErrorWhenUninitialized(t *testing.T) {
	// arrange
	var adapter *MinIOAdapter

	// act
	err := adapter.HealthCheck(nil)

	// assert
	require.EqualError(t, err, "minio is not initialized")
}
