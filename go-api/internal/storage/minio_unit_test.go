//go:build !integration

package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewMinIOAdapter_ReturnsErrorWhenEndpointIsMissing(t *testing.T) {
	// arrange
	cfg := MinIOConfig{
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Bucket:    "contracts",
	}

	// act
	_, err := NewMinIOAdapter(cfg)

	// assert
	require.EqualError(t, err, "minio endpoint is empty")
}

func TestNewMinIOAdapter_ReturnsErrorWhenAccessKeyIsMissing(t *testing.T) {
	// arrange
	cfg := MinIOConfig{
		Endpoint:  "localhost:9000",
		SecretKey: "minioadmin",
		Bucket:    "contracts",
	}

	// act
	_, err := NewMinIOAdapter(cfg)

	// assert
	require.EqualError(t, err, "minio access key is empty")
}

func TestNewMinIOAdapter_ReturnsErrorWhenSecretKeyIsMissing(t *testing.T) {
	// arrange
	cfg := MinIOConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "minioadmin",
		Bucket:    "contracts",
	}

	// act
	_, err := NewMinIOAdapter(cfg)

	// assert
	require.EqualError(t, err, "minio secret key is empty")
}

func TestNewMinIOAdapter_ReturnsErrorWhenBucketIsMissing(t *testing.T) {
	// arrange
	cfg := MinIOConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}

	// act
	_, err := NewMinIOAdapter(cfg)

	// assert
	require.EqualError(t, err, "minio bucket is empty")
}

func TestValidateStorageKey_AllowsValidNestedKey(t *testing.T) {
	// arrange
	key := "documents/contract.pdf"

	// act
	err := validateStorageKey(key)

	// assert
	require.NoError(t, err)
}

func TestValidateStorageKey_AllowsTrimmedKey(t *testing.T) {
	// arrange
	key := "  documents/contract.pdf  "

	// act
	err := validateStorageKey(key)

	// assert
	require.NoError(t, err)
}

func TestValidateStorageKey_ReturnsErrorWhenKeyIsEmpty(t *testing.T) {
	// arrange
	key := " "

	// act
	err := validateStorageKey(key)

	// assert
	require.EqualError(t, err, "storage key is empty")
}

func TestValidateStorageKey_ReturnsErrorWhenKeyIsAbsolute(t *testing.T) {
	// arrange
	key := "/documents/contract.pdf"

	// act
	err := validateStorageKey(key)

	// assert
	require.EqualError(t, err, "absolute storage key is not allowed: \"/documents/contract.pdf\"")
}

func TestValidateStorageKey_ReturnsErrorWhenKeyEscapesRootDirectory(t *testing.T) {
	// arrange
	key := ".."

	// act
	err := validateStorageKey(key)

	// assert
	require.EqualError(t, err, "storage key escapes root: \"..\"")
}

func TestValidateStorageKey_ReturnsErrorWhenNestedKeyEscapesRootDirectory(t *testing.T) {
	// arrange
	key := "../secret.txt"

	// act
	err := validateStorageKey(key)

	// assert
	require.EqualError(t, err, "storage key escapes root: \"../secret.txt\"")
}

func TestMinIOAdapterPut_ReturnsErrorWhenKeyEscapesRootDirectory(t *testing.T) {
	// arrange
	adapter := &MinIOAdapter{bucket: "contracts"}

	// act
	_, err := adapter.Put(context.Background(), "../secret.txt", strings.NewReader("data"))

	// assert
	require.EqualError(t, err, "storage key escapes root: \"../secret.txt\"")
}

func TestMinIOAdapterGet_ReturnsErrorWhenKeyIsAbsolute(t *testing.T) {
	// arrange
	adapter := &MinIOAdapter{bucket: "contracts"}

	// act
	_, err := adapter.Get(context.Background(), "/secret.txt")

	// assert
	require.EqualError(t, err, "absolute storage key is not allowed: \"/secret.txt\"")
}

func TestMinIOAdapterDelete_ReturnsErrorWhenKeyIsEmpty(t *testing.T) {
	// arrange
	adapter := &MinIOAdapter{bucket: "contracts"}

	// act
	err := adapter.Delete(context.Background(), " ")

	// assert
	require.EqualError(t, err, "storage key is empty")
}

func TestIsIgnorableDeleteError_ReturnsTrueForMissingBucket(t *testing.T) {
	// arrange

	// act
	got := isIgnorableDeleteError(minio.ErrorResponse{Code: "NoSuchBucket"})

	// assert
	assert.True(t, got)
}

func TestIsIgnorableDeleteError_ReturnsTrueForMissingObject(t *testing.T) {
	// arrange

	// act
	gotNoSuchKey := isIgnorableDeleteError(minio.ErrorResponse{Code: "NoSuchKey"})
	gotNoSuchObject := isIgnorableDeleteError(minio.ErrorResponse{Code: "NoSuchObject"})

	// assert
	assert.True(t, gotNoSuchKey)
	assert.True(t, gotNoSuchObject)
}

func TestIsIgnorableDeleteError_ReturnsFalseForOtherErrors(t *testing.T) {
	// arrange

	// act
	gotPlainErr := isIgnorableDeleteError(errors.New("boom"))
	gotAccessDenied := isIgnorableDeleteError(minio.ErrorResponse{Code: "AccessDenied"})

	// assert
	assert.False(t, gotPlainErr)
	assert.False(t, gotAccessDenied)
}
