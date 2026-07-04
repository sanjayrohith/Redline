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

type fakeQuarantiner struct {
	quarantinedKey string
	called         bool
	err            error
}

func (f *fakeQuarantiner) Quarantine(_ context.Context, key string) (string, error) {
	f.called = true
	f.quarantinedKey = key
	if f.err != nil {
		return "", f.err
	}
	return "quarantine/" + key, nil
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
	quarantiner := &fakeQuarantiner{}
	pipeline := NewCachePipeline(server.Client(), uploader, quarantiner)

	result, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", sha256HexPipeline(content), "artifacts/model.safetensors", nil)
	if err != nil {
		t.Fatalf("DownloadToCache() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
	if !bytes.Equal(uploader.uploadedData, content) {
		t.Error("uploader did not receive the full streamed content")
	}
	if quarantiner.called {
		t.Error("quarantiner should not be called on a successful verification")
	}
}

func TestCachePipeline_DownloadToCache_DiscardsOnMismatch(t *testing.T) {
	content := []byte("tampered en route")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	uploader := &fakeUploader{}
	quarantiner := &fakeQuarantiner{}
	pipeline := NewCachePipeline(server.Client(), uploader, quarantiner)

	_, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", sha256HexPipeline([]byte("expected something else")), "artifacts/model.safetensors", nil)

	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ChecksumMismatchError", err)
	}
	if !quarantiner.called || quarantiner.quarantinedKey != "artifacts/model.safetensors" {
		t.Errorf("quarantiner called = %v, key = %q, want called with artifacts/model.safetensors", quarantiner.called, quarantiner.quarantinedKey)
	}
}

func TestCachePipeline_DownloadToCache_UpstreamError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{}, &fakeQuarantiner{})

	if _, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", "irrelevant", "artifacts/model.safetensors", nil); err == nil {
		t.Fatal("DownloadToCache() error = nil, want error for 500")
	}
}

func TestCachePipeline_DownloadToCache_UploadError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("data"))
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{err: errors.New("bucket unreachable")}, &fakeQuarantiner{})

	if _, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", "irrelevant", "artifacts/model.safetensors", nil); err == nil {
		t.Fatal("DownloadToCache() error = nil, want propagated upload error")
	}
}

func TestCachePipeline_DownloadToCache_ReportsProgress(t *testing.T) {
	content := bytes.Repeat([]byte("x"), 1000)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{}, &fakeQuarantiner{})

	var calls []int64
	onProgress := func(bytesRead int64) { calls = append(calls, bytesRead) }

	_, err := pipeline.DownloadToCache(context.Background(), server.URL, "sha256", sha256HexPipeline(content), "artifacts/model.safetensors", onProgress)
	if err != nil {
		t.Fatalf("DownloadToCache() error = %v", err)
	}

	if len(calls) == 0 {
		t.Fatal("onProgress was never called")
	}
	if calls[len(calls)-1] != int64(len(content)) {
		t.Errorf("final progress = %d, want %d", calls[len(calls)-1], len(content))
	}
	for i := 1; i < len(calls); i++ {
		if calls[i] < calls[i-1] {
			t.Fatalf("progress went backwards: %v", calls)
		}
	}
}
