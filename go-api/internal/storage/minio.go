package storage

import (
	"context"
	"fmt"
	"io"
	"path"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const minIOPresignExpiry = 24 * time.Hour

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
}

type MinIOAdapter struct {
	client *minio.Client
	bucket string
}

func NewMinIOAdapter(cfg MinIOConfig) (*MinIOAdapter, error) {
	if strings.TrimSpace(cfg.Endpoint) == "" {
		return nil, fmt.Errorf("minio endpoint is empty")
	}
	if strings.TrimSpace(cfg.AccessKey) == "" {
		return nil, fmt.Errorf("minio access key is empty")
	}
	if strings.TrimSpace(cfg.SecretKey) == "" {
		return nil, fmt.Errorf("minio secret key is empty")
	}
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("minio bucket is empty")
	}

	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	return &MinIOAdapter{
		client: client,
		bucket: cfg.Bucket,
	}, nil
}

func (a *MinIOAdapter) Put(ctx context.Context, key string, body io.Reader) (string, error) {
	if err := validateStorageKey(key); err != nil {
		return "", err
	}

	exists, err := a.client.BucketExists(ctx, a.bucket)
	if err != nil {
		return "", fmt.Errorf("check minio bucket: %w", err)
	}
	if !exists {
		if err := a.client.MakeBucket(ctx, a.bucket, minio.MakeBucketOptions{}); err != nil {
			resp := minio.ToErrorResponse(err)
			if resp.Code != "BucketAlreadyOwnedByYou" && resp.Code != "BucketAlreadyExists" {
				return "", fmt.Errorf("create minio bucket: %w", err)
			}
		}
	}

	if _, err := a.client.PutObject(ctx, a.bucket, key, body, -1, minio.PutObjectOptions{}); err != nil {
		return "", fmt.Errorf("put minio object: %w", err)
	}

	url, err := a.client.PresignedGetObject(ctx, a.bucket, key, minIOPresignExpiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign minio object: %w", err)
	}

	return url.String(), nil
}

func (a *MinIOAdapter) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := validateStorageKey(key); err != nil {
		return nil, err
	}

	obj, err := a.client.GetObject(ctx, a.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get minio object: %w", err)
	}
	return obj, nil
}

func (a *MinIOAdapter) Delete(ctx context.Context, key string) error {
	if err := validateStorageKey(key); err != nil {
		return err
	}

	if err := a.client.RemoveObject(ctx, a.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		if isIgnorableDeleteError(err) {
			return nil
		}
		return fmt.Errorf("delete minio object: %w", err)
	}

	return nil
}

func (a *MinIOAdapter) HealthCheck(ctx context.Context) error {
	if a == nil || a.client == nil {
		return fmt.Errorf("minio is not initialized")
	}

	_, err := a.client.BucketExists(ctx, a.bucket)
	if err != nil {
		return fmt.Errorf("check minio bucket: %w", err)
	}

	return nil
}

func validateStorageKey(key string) error {
	cleanKey := path.Clean(strings.TrimSpace(key))
	if cleanKey == "" || cleanKey == "." {
		return fmt.Errorf("storage key is empty")
	}
	if strings.HasPrefix(cleanKey, "/") {
		return fmt.Errorf("absolute storage key is not allowed: %q", key)
	}
	if cleanKey == ".." || strings.HasPrefix(cleanKey, "../") {
		return fmt.Errorf("storage key escapes root: %q", key)
	}
	return nil
}

func isIgnorableDeleteError(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchBucket" || resp.Code == "NoSuchKey" || resp.Code == "NoSuchObject"
}
