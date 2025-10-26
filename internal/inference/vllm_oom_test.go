package inference_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestIsCUDAOutOfMemory_NilError(t *testing.T) {
	if inference.IsCUDAOutOfMemory(nil) {
		t.Error("IsCUDAOutOfMemory(nil) = true, want false")
	}
}

func TestVLLMBackend_Complete_ClassifiesCUDAOutOfMemory(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"torch.cuda.OutOfMemoryError: CUDA out of memory. Tried to allocate 2.00 GiB"}`))
	}))
	defer server.Close()

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	_, err := backend.Complete(context.Background(), inference.CompletionRequest{RequestID: "req-1", Model: "llama"})

	if err == nil {
		t.Fatal("Complete() error = nil, want an error")
	}
	if !inference.IsCUDAOutOfMemory(err) {
		t.Errorf("IsCUDAOutOfMemory(err) = false for error %v, want true", err)
	}
}

func TestVLLMBackend_Complete_UnrelatedServerErrorIsNotClassifiedAsOOM(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal server error: connection reset by peer"}`))
	}))
	defer server.Close()

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	_, err := backend.Complete(context.Background(), inference.CompletionRequest{RequestID: "req-1", Model: "llama"})

	if err == nil {
		t.Fatal("Complete() error = nil, want an error")
	}
	if inference.IsCUDAOutOfMemory(err) {
		t.Errorf("IsCUDAOutOfMemory(err) = true for an unrelated server error %v, want false", err)
	}
}

func TestVLLMBackend_Stream_ClassifiesCUDAOutOfMemoryOnRejectedRequest(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`RuntimeError: CUDA out of memory. GPU 0 has a total capacity of 24.00 GiB`))
	}))
	defer server.Close()

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	_, err := backend.Stream(context.Background(), inference.CompletionRequest{RequestID: "req-1", Model: "llama"})

	if err == nil {
		t.Fatal("Stream() error = nil, want an error")
	}
	if !inference.IsCUDAOutOfMemory(err) {
		t.Errorf("IsCUDAOutOfMemory(err) = false for error %v, want true", err)
	}
}
