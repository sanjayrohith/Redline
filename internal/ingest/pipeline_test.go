package ingest

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/redline/internal/cache"
)

type fakeUploader struct {
	uploadedData []byte
	err          error
}

func (f *fakeUploader) Upload(_ context.Context, _ string, src io.Reader) (*cache.UploadResult, error) {
	if f.err != nil {
		return nil, f.err
	}
	data, err := io.ReadAll(src)
	if err != nil {
		return nil, err
	}
	f.uploadedData = data
	return &cache.UploadResult{ETag: "fake-etag", Size: int64(len(data))}, nil
}

type fakeRemover struct {
	removedKey string
	called     bool
	err        error
}

func (f *fakeRemover) RemoveObject(_ context.Context, key string) error {
	f.called = true
	f.removedKey = key
	return f.err
}

func sha256HexPipeline(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestCachePipeline_DownloadToCache_Success(t *testing.T) {
	content := []byte("streamed straight into the cache")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	uploader := &fakeUploader{}
	remover := &fakeRemover{}
	pipeline := NewCachePipeline(server.Client(), uploader, remover)

	result, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", sha256HexPipeline(content), "artifacts/model.safetensors")
	if err != nil {
		t.Fatalf("DownloadToCache() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
	if !bytes.Equal(uploader.uploadedData, content) {
		t.Error("uploader did not receive the full streamed content")
	}
	if remover.called {
		t.Error("remover should not be called on a successful verification")
	}
}

func TestCachePipeline_DownloadToCache_DiscardsOnMismatch(t *testing.T) {
	content := []byte("tampered en route")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	uploader := &fakeUploader{}
	remover := &fakeRemover{}
	pipeline := NewCachePipeline(server.Client(), uploader, remover)

	_, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", sha256HexPipeline([]byte("expected something else")), "artifacts/model.safetensors")

	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ChecksumMismatchError", err)
	}
	if !remover.called || remover.removedKey != "artifacts/model.safetensors" {
		t.Errorf("remover called = %v, key = %q, want called with artifacts/model.safetensors", remover.called, remover.removedKey)
	}
}

func TestCachePipeline_DownloadToCache_UpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{}, &fakeRemover{})

	if _, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", "irrelevant", "artifacts/model.safetensors"); err == nil {
		t.Fatal("DownloadToCache() error = nil, want error for 500")
	}
}

func TestCachePipeline_DownloadToCache_UploadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{err: errors.New("bucket unreachable")}, &fakeRemover{})

	if _, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", "irrelevant", "artifacts/model.safetensors"); err == nil {
		t.Fatal("DownloadToCache() error = nil, want propagated upload error")
	}
}
