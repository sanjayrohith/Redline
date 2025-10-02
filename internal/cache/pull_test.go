package cache

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestArtifactPuller_PullToDisk(t *testing.T) {
	client := newTestClient(t, "test-pull")
	uploader := NewChunkedUploader(client, 5*1024*1024, 2)
	puller := NewArtifactPuller(client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	content := []byte("weights that live on nvme once pulled")
	if _, err := uploader.Upload(ctx, "artifacts/model.safetensors", bytes.NewReader(content)); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	destPath := filepath.Join(t.TempDir(), "nvme-cache", "model.safetensors")

	n, err := puller.PullToDisk(ctx, "artifacts/model.safetensors", destPath)
	if err != nil {
		t.Fatalf("PullToDisk() error = %v", err)
	}
	if n != int64(len(content)) {
		t.Errorf("n = %d, want %d", n, len(content))
	}

	got, err := os.ReadFile(destPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("read pulled file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("pulled file content does not match the uploaded artifact")
	}
}

func TestArtifactPuller_PullToDisk_MissingObject(t *testing.T) {
	client := newTestClient(t, "test-pull-missing")
	puller := NewArtifactPuller(client)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	destPath := filepath.Join(t.TempDir(), "model.safetensors")

	if _, err := puller.PullToDisk(ctx, "artifacts/does-not-exist.safetensors", destPath); err == nil {
		t.Fatal("PullToDisk() error = nil, want error for a missing object")
	}
	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Error("destPath should not exist after a failed pull")
	}
}

func TestArtifactPuller_PullSet(t *testing.T) {
	client := newTestClient(t, "test-pull-set")
	uploader := NewChunkedUploader(client, 5*1024*1024, 2)
	puller := NewArtifactPuller(client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	fixtures := map[string][]byte{
		"artifacts/config.json":       []byte(`{"model_type":"llama"}`),
		"artifacts/model.safetensors": bytes.Repeat([]byte("w"), 1024),
		"artifacts/tokenizer.json":    []byte(`{"version":"1.0"}`),
	}
	for key, content := range fixtures {
		if _, err := uploader.Upload(ctx, key, bytes.NewReader(content)); err != nil {
			t.Fatalf("Upload(%s) error = %v", key, err)
		}
	}

	nvmeDir := t.TempDir()
	files := map[string]string{
		"artifacts/config.json":       filepath.Join(nvmeDir, "config.json"),
		"artifacts/model.safetensors": filepath.Join(nvmeDir, "model.safetensors"),
		"artifacts/tokenizer.json":    filepath.Join(nvmeDir, "tokenizer.json"),
	}

	if err := puller.PullSet(ctx, files, 2); err != nil {
		t.Fatalf("PullSet() error = %v", err)
	}

	for key, destPath := range files {
		got, err := os.ReadFile(destPath) // #nosec G304 -- test-controlled path
		if err != nil {
			t.Fatalf("read %s: %v", destPath, err)
		}
		if !bytes.Equal(got, fixtures[key]) {
			t.Errorf("%s content mismatch", destPath)
		}
	}
}

func TestArtifactPuller_PullSet_FailsOnMissingObject(t *testing.T) {
	client := newTestClient(t, "test-pull-set-fail")
	uploader := NewChunkedUploader(client, 5*1024*1024, 2)
	puller := NewArtifactPuller(client)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	if _, err := uploader.Upload(ctx, "artifacts/config.json", bytes.NewReader([]byte("{}"))); err != nil {
		t.Fatalf("Upload() error = %v", err)
	}

	nvmeDir := t.TempDir()
	files := map[string]string{
		"artifacts/config.json":  filepath.Join(nvmeDir, "config.json"),
		"artifacts/missing.file": filepath.Join(nvmeDir, "missing.file"),
	}

	if err := puller.PullSet(ctx, files, 2); err == nil {
		t.Fatal("PullSet() error = nil, want error for a missing object in the set")
	}
}
