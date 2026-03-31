//go:build !integration

package storage

import (
	"context"
	"strings"
	"testing"
)

func TestNewMinIOAdapterRequiresFields(t *testing.T) {
	testCases := []struct {
		name string
		cfg  MinIOConfig
		want string
	}{
		{
			name: "endpoint",
			cfg: MinIOConfig{
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
				Bucket:    "contracts",
			},
			want: "minio endpoint is empty",
		},
		{
			name: "access key",
			cfg: MinIOConfig{
				Endpoint:  "localhost:9000",
				SecretKey: "minioadmin",
				Bucket:    "contracts",
			},
			want: "minio access key is empty",
		},
		{
			name: "secret key",
			cfg: MinIOConfig{
				Endpoint:  "localhost:9000",
				AccessKey: "minioadmin",
				Bucket:    "contracts",
			},
			want: "minio secret key is empty",
		},
		{
			name: "bucket",
			cfg: MinIOConfig{
				Endpoint:  "localhost:9000",
				AccessKey: "minioadmin",
				SecretKey: "minioadmin",
			},
			want: "minio bucket is empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewMinIOAdapter(tc.cfg)
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.Error() != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestValidateStorageKey(t *testing.T) {
	testCases := []struct {
		name    string
		key     string
		wantErr string
	}{
		{name: "valid nested key", key: "documents/contract.pdf"},
		{name: "trimmed key", key: "  documents/contract.pdf  "},
		{name: "empty", key: " ", wantErr: "storage key is empty"},
		{name: "absolute", key: "/documents/contract.pdf", wantErr: "absolute storage key is not allowed: \"/documents/contract.pdf\""},
		{name: "parent root", key: "..", wantErr: "storage key escapes root: \"..\""},
		{name: "parent nested", key: "../secret.txt", wantErr: "storage key escapes root: \"../secret.txt\""},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateStorageKey(tc.key)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.Error() != tc.wantErr {
				t.Fatalf("expected %q, got %q", tc.wantErr, err.Error())
			}
		})
	}
}

func TestMinIOAdapterMethodsRejectInvalidKeys(t *testing.T) {
	adapter := &MinIOAdapter{bucket: "contracts"}
	testCases := []struct {
		name string
		call func() error
		want string
	}{
		{
			name: "put",
			call: func() error {
				_, err := adapter.Put(context.Background(), "../secret.txt", strings.NewReader("data"))
				return err
			},
			want: "storage key escapes root: \"../secret.txt\"",
		},
		{
			name: "get",
			call: func() error {
				_, err := adapter.Get(context.Background(), "/secret.txt")
				return err
			},
			want: "absolute storage key is not allowed: \"/secret.txt\"",
		},
		{
			name: "delete",
			call: func() error {
				return adapter.Delete(context.Background(), " ")
			},
			want: "storage key is empty",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			if err == nil {
				t.Fatal("expected validation error")
			}
			if err.Error() != tc.want {
				t.Fatalf("expected %q, got %q", tc.want, err.Error())
			}
		})
	}
}
