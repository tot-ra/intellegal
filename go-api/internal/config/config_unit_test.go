//go:build !integration

package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoadDefaultsWithToken(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STORAGE_PROVIDER", "")
	t.Setenv("LOCAL_STORAGE_PATH", "")
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_BUCKET", "")
	t.Setenv("DATABASE_PING_TIMEOUT", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.Port != 8080 {
		t.Fatalf("expected default port 8080, got %d", cfg.Port)
	}
	if cfg.StorageProvider != "minio" {
		t.Fatalf("expected default storage provider minio, got %q", cfg.StorageProvider)
	}
	if cfg.MinIOEndpoint != "minio:9000" {
		t.Fatalf("expected default minio endpoint, got %q", cfg.MinIOEndpoint)
	}
	if cfg.MinIOBucket != "contracts" {
		t.Fatalf("expected default minio bucket, got %q", cfg.MinIOBucket)
	}
	if cfg.DatabasePingTimeout != 5*time.Second {
		t.Fatalf("expected default database ping timeout 5s, got %s", cfg.DatabasePingTimeout)
	}
	if cfg.InternalAITimeout != 90*time.Second {
		t.Fatalf("expected default internal ai timeout 90s, got %s", cfg.InternalAITimeout)
	}
	if len(cfg.CORSAllowedOrigins) == 0 {
		t.Fatal("expected default cors allowed origins")
	}
	if cfg.CORSAllowedOrigins[0] != "http://localhost:3000" {
		t.Fatalf("expected localhost:3000 as first default cors origin, got %q", cfg.CORSAllowedOrigins[0])
	}
}

func TestLoadFailsWithoutToken(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgresql://app:app@localhost:5432/app")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error when INTERNAL_SERVICE_TOKEN is missing")
	}
}

func TestLoadParsesLogLevel(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("LOG_LEVEL", "debug")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if cfg.LogLevel != "debug" {
		t.Fatalf("expected debug log level, got %s", cfg.LogLevel)
	}
}

func TestLoadRejectsInvalidStorageProvider(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STORAGE_PROVIDER", "s3")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid storage provider")
	}
	if !strings.Contains(err.Error(), "invalid STORAGE_PROVIDER") {
		t.Fatalf("expected storage provider validation error, got %v", err)
	}
}

func TestLoadRejectsInvalidDatabasePingTimeout(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("DATABASE_PING_TIMEOUT", "zero")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid database ping timeout")
	}
}

func TestLoadRejectsInvalidInternalAITimeout(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("INTERNAL_AI_TIMEOUT", "zero")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid internal ai timeout")
	}
}

func TestLoadRejectsInvalidExternalCopyRetries(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("EXTERNAL_COPY_API_RETRIES", "0")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for invalid external copy retries")
	}
}

func TestLoadRequiresAzureFields(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STORAGE_PROVIDER", "azure")
	t.Setenv("AZURE_STORAGE_ACCOUNT", "")
	t.Setenv("AZURE_BLOB_CONTAINER", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing azure storage fields")
	}
}

func TestLoadRequiresMinIOFields(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("STORAGE_PROVIDER", "minio")
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for missing minio storage fields")
	}
}

func TestLoadParsesCORSAllowedOrigins(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000, https://app.example.com")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}

	if len(cfg.CORSAllowedOrigins) != 2 {
		t.Fatalf("expected 2 cors origins, got %d", len(cfg.CORSAllowedOrigins))
	}
	if cfg.CORSAllowedOrigins[1] != "https://app.example.com" {
		t.Fatalf("unexpected second cors origin: %q", cfg.CORSAllowedOrigins[1])
	}
}

func TestLoadRejectsEmptyCORSAllowedOrigins(t *testing.T) {
	setRequiredEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", " , ")

	_, err := Load()
	if err == nil {
		t.Fatal("expected error for empty CORS_ALLOWED_ORIGINS")
	}
}

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgresql://app:app@localhost:5432/app")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-token")
	t.Setenv("MINIO_ACCESS_KEY", "minioadmin")
	t.Setenv("MINIO_SECRET_KEY", "minioadmin")
}
