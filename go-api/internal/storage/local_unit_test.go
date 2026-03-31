//go:build !integration

package storage

import (
	"context"
	"io"
	"strings"
	"testing"
)

func TestLocalAdapterPutAndGet(t *testing.T) {
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("expected adapter, got error: %v", err)
	}

	uri, err := adapter.Put(context.Background(), "docs/contract.txt", strings.NewReader("sample"))
	if err != nil {
		t.Fatalf("expected put success, got error: %v", err)
	}
	if !strings.HasPrefix(uri, "file://") {
		t.Fatalf("expected file URI, got %q", uri)
	}

	rc, err := adapter.Get(context.Background(), "docs/contract.txt")
	if err != nil {
		t.Fatalf("expected get success, got error: %v", err)
	}
	defer rc.Close()

	b, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read object: %v", err)
	}
	if string(b) != "sample" {
		t.Fatalf("expected object payload sample, got %q", string(b))
	}
}

func TestLocalAdapterRejectsPathEscape(t *testing.T) {
	adapter, err := NewLocalAdapter(t.TempDir())
	if err != nil {
		t.Fatalf("expected adapter, got error: %v", err)
	}

	_, err = adapter.Put(context.Background(), "../outside.txt", strings.NewReader("nope"))
	if err == nil {
		t.Fatal("expected path traversal error")
	}
}
