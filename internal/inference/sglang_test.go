package inference_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestSGLangBackend_CompleteAgainstOpenAICompatibleServer(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"cached prefix response"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":3}}`))
	}))
	defer server.Close()

	backend := inference.NewSGLangBackend(server.URL, server.Client())
	resp, err := backend.Complete(context.Background(), inference.CompletionRequest{
		RequestID: "req-1",
		Model:     "llama",
		Messages:  []inference.Message{{Role: "system", Content: "shared system prompt"}, {Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Content != "cached prefix response" {
		t.Errorf("Content = %q, want %q", resp.Content, "cached prefix response")
	}
}

func TestSGLangBackend_ImplementsBackend(t *testing.T) {
	var _ inference.Backend = inference.NewSGLangBackend("http://example.invalid", http.DefaultClient)
}
