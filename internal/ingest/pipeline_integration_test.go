package ingest_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	tcminio "github.com/testcontainers/testcontainers-go/modules/minio"

	"github.com/sanjayrohith/redline/internal/cache"
	"github.com/sanjayrohith/redline/internal/ingest"
)

func TestCachePipeline_DownloadToCache_RealMinIORoundTrip(t *testing.T) {
	ctx := context.Background()

	const user, password = "minioadmin", "minioadmin"
	container, err := tcminio.Run(ctx, "minio/minio:latest",
		tcminio.WithUsername(user),
		tcminio.WithPassword(password),
	)
	if err != nil {
		t.Skipf("skipping integration test: could not start minio container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	endpoint, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	setupCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	client, err := cache.NewClient(setupCtx, cache.Options{
		Endpoint: endpoint, AccessKeyID: user, SecretAccessKey: password, BucketName: "pipeline-test",
	})
	if err != nil {
		t.Fatalf("cache.NewClient() error = %v", err)
	}
	uploader := cache.NewChunkedUploader(client, 5*1024*1024, 2)

	content := []byte("a real model weight file's bytes, streamed end to end")
	sum := sha256.Sum256(content)
	digest := hex.EncodeToString(sum[:])

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	pipeline := ingest.NewCachePipeline(server.Client(), uploader, client)

	result, err := pipeline.DownloadToCache(ctx, server.URL, "sha256", digest, "artifacts/real-model.safetensors", nil)
	if err != nil {
		t.Fatalf("DownloadToCache() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}

	obj, err := client.GetObject(ctx, "artifacts/real-model.safetensors")
	if err != nil {
		t.Fatalf("GetObject() error = %v", err)
	}
	defer func() { _ = obj.Close() }()

	got, err := io.ReadAll(obj)
	if err != nil {
		t.Fatalf("read cached object: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("cached object content does not match the source")
	}
}
