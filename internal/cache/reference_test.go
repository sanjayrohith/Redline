package cache

import (
	"bytes"
	"context"
	"testing"
	"time"
)

func TestClient_MarkUnreferencedAndReferenced(t *testing.T) {
	client := newTestClient(t, "test-reference")
	uploader := NewChunkedUploader(client, 5*1024*1024, 1)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	if _, err := uploader.Upload(ctx, "artifacts/model.safetensors", bytes.NewReader([]byte("weights"))); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	unreferenced, err := client.IsUnreferenced(ctx, "artifacts/model.safetensors")
	if err != nil {
		t.Fatalf("IsUnreferenced() error = %v", err)
	}
	if unreferenced {
		t.Error("freshly uploaded object should not start out tagged unreferenced")
	}

	if err := client.MarkUnreferenced(ctx, "artifacts/model.safetensors"); err != nil {
		t.Fatalf("MarkUnreferenced() error = %v", err)
	}
	unreferenced, err = client.IsUnreferenced(ctx, "artifacts/model.safetensors")
	if err != nil {
		t.Fatalf("IsUnreferenced() error = %v", err)
	}
	if !unreferenced {
		t.Error("object should be tagged unreferenced after MarkUnreferenced")
	}

	if err := client.MarkReferenced(ctx, "artifacts/model.safetensors"); err != nil {
		t.Fatalf("MarkReferenced() error = %v", err)
	}
	unreferenced, err = client.IsUnreferenced(ctx, "artifacts/model.safetensors")
	if err != nil {
		t.Fatalf("IsUnreferenced() error = %v", err)
	}
	if unreferenced {
		t.Error("object should no longer be tagged unreferenced after MarkReferenced")
	}
}
