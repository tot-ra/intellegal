//go:build !integration

package storage

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestNewMinIOAdapter_ReturnsErrorWhenEndpointIsMissing(t *testing.T) {
	// Arrange
	cfg := MinIOConfig{
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		Bucket:    "contracts",
	}

	// Act
	_, err := NewMinIOAdapter(cfg)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "minio endpoint is empty" {
		t.Fatalf("expected %q, got %q", "minio endpoint is empty", err.Error())
	}
}

func TestNewMinIOAdapter_ReturnsErrorWhenAccessKeyIsMissing(t *testing.T) {
	// Arrange
	cfg := MinIOConfig{
		Endpoint:  "localhost:9000",
		SecretKey: "minioadmin",
		Bucket:    "contracts",
	}

	// Act
	_, err := NewMinIOAdapter(cfg)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "minio access key is empty" {
		t.Fatalf("expected %q, got %q", "minio access key is empty", err.Error())
	}
}

func TestNewMinIOAdapter_ReturnsErrorWhenSecretKeyIsMissing(t *testing.T) {
	// Arrange
	cfg := MinIOConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "minioadmin",
		Bucket:    "contracts",
	}

	// Act
	_, err := NewMinIOAdapter(cfg)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "minio secret key is empty" {
		t.Fatalf("expected %q, got %q", "minio secret key is empty", err.Error())
	}
}

func TestNewMinIOAdapter_ReturnsErrorWhenBucketIsMissing(t *testing.T) {
	// Arrange
	cfg := MinIOConfig{
		Endpoint:  "localhost:9000",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
	}

	// Act
	_, err := NewMinIOAdapter(cfg)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "minio bucket is empty" {
		t.Fatalf("expected %q, got %q", "minio bucket is empty", err.Error())
	}
}

func TestValidateStorageKey_AllowsValidNestedKey(t *testing.T) {
	// Arrange
	key := "documents/contract.pdf"

	// Act
	err := validateStorageKey(key)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateStorageKey_AllowsTrimmedKey(t *testing.T) {
	// Arrange
	key := "  documents/contract.pdf  "

	// Act
	err := validateStorageKey(key)

	// Assert
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}

func TestValidateStorageKey_ReturnsErrorWhenKeyIsEmpty(t *testing.T) {
	// Arrange
	key := " "

	// Act
	err := validateStorageKey(key)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "storage key is empty" {
		t.Fatalf("expected %q, got %q", "storage key is empty", err.Error())
	}
}

func TestValidateStorageKey_ReturnsErrorWhenKeyIsAbsolute(t *testing.T) {
	// Arrange
	key := "/documents/contract.pdf"

	// Act
	err := validateStorageKey(key)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "absolute storage key is not allowed: \"/documents/contract.pdf\"" {
		t.Fatalf("expected %q, got %q", "absolute storage key is not allowed: \"/documents/contract.pdf\"", err.Error())
	}
}

func TestValidateStorageKey_ReturnsErrorWhenKeyEscapesRootDirectory(t *testing.T) {
	// Arrange
	key := ".."

	// Act
	err := validateStorageKey(key)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "storage key escapes root: \"..\"" {
		t.Fatalf("expected %q, got %q", "storage key escapes root: \"..\"", err.Error())
	}
}

func TestValidateStorageKey_ReturnsErrorWhenNestedKeyEscapesRootDirectory(t *testing.T) {
	// Arrange
	key := "../secret.txt"

	// Act
	err := validateStorageKey(key)

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "storage key escapes root: \"../secret.txt\"" {
		t.Fatalf("expected %q, got %q", "storage key escapes root: \"../secret.txt\"", err.Error())
	}
}

func TestMinIOAdapterPut_ReturnsErrorWhenKeyEscapesRootDirectory(t *testing.T) {
	// Arrange
	adapter := &MinIOAdapter{bucket: "contracts"}

	// Act
	_, err := adapter.Put(context.Background(), "../secret.txt", strings.NewReader("data"))

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "storage key escapes root: \"../secret.txt\"" {
		t.Fatalf("expected %q, got %q", "storage key escapes root: \"../secret.txt\"", err.Error())
	}
}

func TestMinIOAdapterGet_ReturnsErrorWhenKeyIsAbsolute(t *testing.T) {
	// Arrange
	adapter := &MinIOAdapter{bucket: "contracts"}

	// Act
	_, err := adapter.Get(context.Background(), "/secret.txt")

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "absolute storage key is not allowed: \"/secret.txt\"" {
		t.Fatalf("expected %q, got %q", "absolute storage key is not allowed: \"/secret.txt\"", err.Error())
	}
}

func TestMinIOAdapterDelete_ReturnsErrorWhenKeyIsEmpty(t *testing.T) {
	// Arrange
	adapter := &MinIOAdapter{bucket: "contracts"}

	// Act
	err := adapter.Delete(context.Background(), " ")

	// Assert
	if err == nil {
		t.Fatal("expected validation error")
	}
	if err.Error() != "storage key is empty" {
		t.Fatalf("expected %q, got %q", "storage key is empty", err.Error())
	}
}

func TestIsIgnorableDeleteError_ReturnsTrueForMissingBucket(t *testing.T) {
	if !isIgnorableDeleteError(minio.ErrorResponse{Code: "NoSuchBucket"}) {
		t.Fatal("expected NoSuchBucket to be ignored")
	}
}

func TestIsIgnorableDeleteError_ReturnsTrueForMissingObject(t *testing.T) {
	if !isIgnorableDeleteError(minio.ErrorResponse{Code: "NoSuchKey"}) {
		t.Fatal("expected NoSuchKey to be ignored")
	}
	if !isIgnorableDeleteError(minio.ErrorResponse{Code: "NoSuchObject"}) {
		t.Fatal("expected NoSuchObject to be ignored")
	}
}

func TestIsIgnorableDeleteError_ReturnsFalseForOtherErrors(t *testing.T) {
	if isIgnorableDeleteError(errors.New("boom")) {
		t.Fatal("expected plain errors to remain fatal")
	}
	if isIgnorableDeleteError(minio.ErrorResponse{Code: "AccessDenied"}) {
		t.Fatal("expected AccessDenied to remain fatal")
	}
}
