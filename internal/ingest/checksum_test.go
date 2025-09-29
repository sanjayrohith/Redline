package ingest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func TestStreamAndVerify_MatchingDigest(t *testing.T) {
	content := []byte("weights go here")
	var dst bytes.Buffer

	err := StreamAndVerify(&dst, bytes.NewReader(content), "sha256", sha256Hex(content), "model.safetensors")
	if err != nil {
		t.Fatalf("StreamAndVerify() error = %v", err)
	}
	if dst.String() != string(content) {
		t.Error("dst did not receive the full content")
	}
}

func TestStreamAndVerify_MismatchedDigest(t *testing.T) {
	content := []byte("weights go here")
	var dst bytes.Buffer

	err := StreamAndVerify(&dst, bytes.NewReader(content), "sha256", "0000000000000000000000000000000000000000000000000000000000000000", "model.safetensors")

	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ChecksumMismatchError", err)
	}
	if mismatch.Path != "model.safetensors" {
		t.Errorf("Path = %q, want model.safetensors", mismatch.Path)
	}
}

func TestStreamAndVerify_UnsupportedAlgorithm(t *testing.T) {
	var dst bytes.Buffer
	if err := StreamAndVerify(&dst, bytes.NewReader(nil), "md5", "irrelevant", "file"); err == nil {
		t.Fatal("StreamAndVerify() error = nil, want error for unsupported algorithm")
	}
}

func TestDownloadAndVerify_Success(t *testing.T) {
	content := []byte("safetensors payload bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "model.safetensors")

	err := DownloadAndVerify(t.Context(), server.Client(), server.URL, "sha256", sha256Hex(content), destPath)
	if err != nil {
		t.Fatalf("DownloadAndVerify() error = %v", err)
	}

	got, err := os.ReadFile(destPath) // #nosec G304 -- test-controlled path
	if err != nil {
		t.Fatalf("read downloaded file: %v", err)
	}
	if !bytes.Equal(got, content) {
		t.Error("downloaded file content does not match source")
	}
}

func TestDownloadAndVerify_DiscardsFileOnMismatch(t *testing.T) {
	content := []byte("tampered payload")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write(content)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "model.safetensors")

	err := DownloadAndVerify(t.Context(), server.Client(), server.URL, "sha256", sha256Hex([]byte("expected different content")), destPath)

	var mismatch *ChecksumMismatchError
	if !errors.As(err, &mismatch) {
		t.Fatalf("error = %v, want *ChecksumMismatchError", err)
	}

	if _, statErr := os.Stat(destPath); !os.IsNotExist(statErr) {
		t.Error("destPath should have been removed after a checksum mismatch")
	}
}

func TestDownloadAndVerify_ServerError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	destPath := filepath.Join(t.TempDir(), "model.safetensors")

	if err := DownloadAndVerify(t.Context(), server.Client(), server.URL, "sha256", "anything", destPath); err == nil {
		t.Fatal("DownloadAndVerify() error = nil, want error for 500")
	}
}
