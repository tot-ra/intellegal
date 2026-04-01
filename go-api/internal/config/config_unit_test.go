//go:build !integration

package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLoad_ReturnsDefaultsWhenRequiredTokenIsPresent(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_BUCKET", "")
	t.Setenv("DATABASE_PING_TIMEOUT", "")

	// act
	cfg, err := Load()
	require.NoError(t, err)

	// assert
	assert.Equal(t, 8080, cfg.Port)
	assert.Equal(t, "minio:9000", cfg.MinIOEndpoint)
	assert.Equal(t, "contracts", cfg.MinIOBucket)
	assert.Equal(t, 5*time.Second, cfg.DatabasePingTimeout)
	assert.Equal(t, 5*time.Minute, cfg.InternalAITimeout)
	require.NotEmpty(t, cfg.CORSAllowedOrigins)
	assert.Equal(t, "http://localhost:3000", cfg.CORSAllowedOrigins[0])
}

func TestLoad_ReturnsErrorWhenInternalServiceTokenIsMissing(t *testing.T) {
	// arrange
	t.Setenv("DATABASE_URL", "postgresql://app:app@localhost:5432/app")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "")

	// act
	_, err := Load()

	// assert
	require.Error(t, err)
}

func TestLoad_ParsesLogLevel(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("LOG_LEVEL", "debug")

	// act
	cfg, err := Load()
	require.NoError(t, err)

	// assert
	assert.Equal(t, "debug", string(cfg.LogLevel))
}

func TestLoad_ReturnsErrorForInvalidDatabasePingTimeout(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("DATABASE_PING_TIMEOUT", "zero")

	// act
	_, err := Load()

	// assert
	require.Error(t, err)
}

func TestLoad_ReturnsErrorForInvalidInternalAITimeout(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("INTERNAL_AI_TIMEOUT", "zero")

	// act
	_, err := Load()

	// assert
	require.Error(t, err)
}

func TestLoad_ReturnsErrorForInvalidExternalCopyRetries(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("EXTERNAL_COPY_API_RETRIES", "0")

	// act
	_, err := Load()

	// assert
	require.Error(t, err)
}

func TestLoad_ReturnsErrorWhenRequiredMinIOFieldsAreMissing(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")

	// act
	_, err := Load()

	// assert
	require.Error(t, err)
}

func TestLoad_ParsesCORSAllowedOrigins(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", "http://localhost:3000, https://app.example.com")

	// act
	cfg, err := Load()
	require.NoError(t, err)

	// assert
	require.Len(t, cfg.CORSAllowedOrigins, 2)
	assert.Equal(t, "https://app.example.com", cfg.CORSAllowedOrigins[1])
}

func TestLoad_ReturnsErrorWhenCORSAllowedOriginsAreEmpty(t *testing.T) {
	// arrange
	setRequiredEnv(t)
	t.Setenv("CORS_ALLOWED_ORIGINS", " , ")

	// act
	_, err := Load()

	// assert
	require.Error(t, err)
}

func setRequiredEnv(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgresql://app:app@localhost:5432/app")
	t.Setenv("INTERNAL_SERVICE_TOKEN", "test-token")
	t.Setenv("MINIO_ACCESS_KEY", "minioadmin")
	t.Setenv("MINIO_SECRET_KEY", "minioadmin")
}
