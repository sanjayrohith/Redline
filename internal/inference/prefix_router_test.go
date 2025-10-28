package inference_test

import (
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestPrefixRouter_RoutesMatchingSharedSystemPromptToSGLang(t *testing.T) {
	sglang := inference.NewMockBackend(0)
	fallback := inference.NewMockBackend(0)
	router := inference.NewPrefixRouter(sglang, fallback, []string{"you are a helpful assistant"})

	req := inference.CompletionRequest{
		Messages: []inference.Message{{Role: "system", Content: "you are a helpful assistant"}, {Role: "user", Content: "hi"}},
	}

	if got := router.Select(req); got != inference.Backend(sglang) {
		t.Errorf("Select() did not route a matching shared system prompt to sglang")
	}
}

func TestPrefixRouter_RoutesUnmatchedPromptToFallback(t *testing.T) {
	sglang := inference.NewMockBackend(0)
	fallback := inference.NewMockBackend(0)
	router := inference.NewPrefixRouter(sglang, fallback, []string{"you are a helpful assistant"})

	req := inference.CompletionRequest{
		Messages: []inference.Message{{Role: "system", Content: "a different prompt entirely"}, {Role: "user", Content: "hi"}},
	}

	if got := router.Select(req); got != inference.Backend(fallback) {
		t.Errorf("Select() did not route an unmatched prompt to fallback")
	}
}

func TestPrefixRouter_RoutesRequestsWithNoSystemMessageToFallback(t *testing.T) {
	sglang := inference.NewMockBackend(0)
	fallback := inference.NewMockBackend(0)
	router := inference.NewPrefixRouter(sglang, fallback, []string{"you are a helpful assistant"})

	req := inference.CompletionRequest{Messages: []inference.Message{{Role: "user", Content: "hi"}}}

	if got := router.Select(req); got != inference.Backend(fallback) {
		t.Errorf("Select() did not route a request with no system message to fallback")
	}
}
