package cache

import (
	"bytes"
	"context"
	"io"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestQuarantine_MovesObjectAndPurgesOriginal(t *testing.T) {
	client := newTestClient(t, "quarantine-test")
	ctx := context.Background()

	content := []byte("this artifact failed checksum verification")
	if _, err := client.minio.PutObject(ctx, client.bucket, "artifacts/bad.safetensors",
		bytes.NewReader(content), int64(len(content)), minio.PutObjectOptions{}); err != nil {
		t.Fatalf("seed object: %v", err)
	}

	quarantineKey, err := client.Quarantine(ctx, "artifacts/bad.safetensors")
	if err != nil {
		t.Fatalf("Quarantine() error = %v", err)
	}
	if quarantineKey != "quarantine/artifacts/bad.safetensors" {
		t.Errorf("quarantineKey = %q, want quarantine/artifacts/bad.safetensors", quarantineKey)
	}

	// The original key must be gone. GetObject itself is lazy (it does not
	// make a request until the object is read or stat'd), so the absence
	// only surfaces once actually read.
	orig, err := client.GetObject(ctx, "artifacts/bad.safetensors")
	if err == nil {
		defer func() { _ = orig.Close() }()
		if _, readErr := io.ReadAll(orig); readErr == nil {
			t.Error("original object still readable after quarantine")
		}
	}

	// The quarantined copy must hold the same bytes, for operator inspection.
	obj, err := client.GetObject(ctx, quarantineKey)
	if err != nil {
		t.Fatalf("GetObject(quarantined) error = %v", err)
	}
	defer func() { _ = obj.Close() }()

	got, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read quarantined object: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("quarantined object content does not match the original")
	}
}

func TestQuarantine_MissingObjectErrors(t *testing.T) {
	client := newTestClient(t, "quarantine-missing-test")

	if _, err := client.Quarantine(context.Background(), "artifacts/never-existed.safetensors"); err == nil {
		t.Fatal("Quarantine() error = nil, want an error for a nonexistent object")
	}
}
