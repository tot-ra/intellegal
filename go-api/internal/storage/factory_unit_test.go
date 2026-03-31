//go:build !integration

package storage

import "testing"

func TestNewAdapterMinIO(t *testing.T) {
	adapter, err := NewAdapter(FactoryConfig{
		MinIOEndpoint:  "localhost:9000",
		MinIOAccessKey: "minioadmin",
		MinIOSecretKey: "minioadmin",
		MinIOBucket:    "contracts",
	})
	if err != nil {
		t.Fatalf("expected minio adapter, got error: %v", err)
	}
	if _, ok := adapter.(*MinIOAdapter); !ok {
		t.Fatalf("expected MinIOAdapter type, got %T", adapter)
	}
}
