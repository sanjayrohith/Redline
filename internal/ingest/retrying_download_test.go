package ingest

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/scheduler"
)

func fastRetryPolicy(maxAttempts int) scheduler.RetryPolicy {
	return scheduler.RetryPolicy{BaseDelay: time.Millisecond, MaxDelay: 5 * time.Millisecond, MaxAttempts: maxAttempts}
}

func TestRetryingDownload_SucceedsOnFirstAttemptWithoutRetry(t *testing.T) {
	content := []byte("good content")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	uploader := &fakeUploader{}
	quarantiner := &fakeQuarantiner{}
	pipeline := NewCachePipeline(server.Client(), uploader, quarantiner)

	sum := sha256.Sum256(content)
	result, err := RetryingDownload(context.Background(), pipeline, fastRetryPolicy(3),
		server.URL, "sha256", hex.EncodeToString(sum[:]), "artifacts/model.safetensors", nil)
	if err != nil {
		t.Fatalf("RetryingDownload() error = %v", err)
	}
	if result.Size != int64(len(content)) {
		t.Errorf("Size = %d, want %d", result.Size, len(content))
	}
	if quarantiner.called {
		t.Error("quarantiner should not be called when the checksum matches on the first attempt")
	}
}

func TestRetryingDownload_RetriesAfterChecksumMismatchThenSucceeds(t *testing.T) {
	goodContent := []byte("the real content")
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		if requestCount == 1 {
			_, _ = w.Write([]byte("corrupted on the wire"))
			return
		}
		_, _ = w.Write(goodContent)
	}))
	defer server.Close()

	quarantiner := &fakeQuarantiner{}
	pipeline := NewCachePipeline(server.Client(), &fakeUploader{}, quarantiner)

	sum := sha256.Sum256(goodContent)
	result, err := RetryingDownload(context.Background(), pipeline, fastRetryPolicy(3),
		server.URL, "sha256", hex.EncodeToString(sum[:]), "artifacts/model.safetensors", nil)
	if err != nil {
		t.Fatalf("RetryingDownload() error = %v", err)
	}
	if result.Size != int64(len(goodContent)) {
		t.Errorf("Size = %d, want %d", result.Size, len(goodContent))
	}
	if requestCount != 2 {
		t.Errorf("requestCount = %d, want 2 (one failed attempt, one retry)", requestCount)
	}
	if !quarantiner.called {
		t.Error("the first, corrupted attempt should have been quarantined")
	}
}

func TestRetryingDownload_GivesUpAfterMaxAttempts(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		_, _ = w.Write([]byte("always corrupted"))
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{}, &fakeQuarantiner{})

	_, err := RetryingDownload(context.Background(), pipeline, fastRetryPolicy(3),
		server.URL, "sha256", "0000000000000000000000000000000000000000000000000000000000000000", "artifacts/model.safetensors", nil)

	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ChecksumMismatchError after exhausting retries", err)
	}
	if requestCount != 3 {
		t.Errorf("requestCount = %d, want 3 (MaxAttempts)", requestCount)
	}
}

func TestRetryingDownload_NonChecksumErrorIsNotRetried(t *testing.T) {
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	pipeline := NewCachePipeline(server.Client(), &fakeUploader{}, &fakeQuarantiner{})

	_, err := RetryingDownload(context.Background(), pipeline, fastRetryPolicy(3),
		server.URL, "sha256", "irrelevant", "artifacts/model.safetensors", nil)
	if err == nil {
		t.Fatal("RetryingDownload() error = nil, want an error")
	}
	if requestCount != 1 {
		t.Errorf("requestCount = %d, want 1 (an upstream error is not a checksum mismatch, so it must not retry)", requestCount)
	}
}
