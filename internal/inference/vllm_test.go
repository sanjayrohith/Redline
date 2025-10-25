package inference_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestVLLMBackend_CompleteParsesANonStreamingResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatalf("decode request body: %v", err)
		}
		if body["stream"] != false {
			t.Errorf("Stream = %v, want false", body["stream"])
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "hello there"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 5, "completion_tokens": 2}
		}`)
	}))
	defer server.Close()

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	resp, err := backend.Complete(context.Background(), inference.CompletionRequest{
		RequestID: "req-1",
		Model:     "llama",
		Messages:  []inference.Message{{Role: "user", Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if resp.Content != "hello there" {
		t.Errorf("Content = %q, want %q", resp.Content, "hello there")
	}
	if resp.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.FinishReason)
	}
	if resp.Usage.PromptTokens != 5 || resp.Usage.CompletionTokens != 2 {
		t.Errorf("Usage = %+v, want {5 2}", resp.Usage)
	}
}

func TestVLLMBackend_CompleteSurfacesNon200Status(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	_, err := backend.Complete(context.Background(), inference.CompletionRequest{RequestID: "req-1", Model: "llama"})
	if err == nil {
		t.Fatal("Complete() error = nil, want an error for a 500 response")
	}
}

func writeSSE(w http.ResponseWriter, flusher http.Flusher, payload string) {
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
}

func TestVLLMBackend_StreamEmitsIncrementalContentThenDone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] != true {
			t.Errorf("Stream = %v, want true", body["stream"])
		}

		w.Header().Set("Content-Type", "text/event-stream")
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Fatal("ResponseWriter does not support flushing")
		}

		writeSSE(w, flusher, `{"choices":[{"index":0,"delta":{"role":"assistant","content":"hel"},"finish_reason":null}]}`)
		writeSSE(w, flusher, `{"choices":[{"index":0,"delta":{"content":"lo"},"finish_reason":null}]}`)
		writeSSE(w, flusher, `{"choices":[{"index":0,"delta":{"content":""},"finish_reason":"stop"}],"usage":{"prompt_tokens":3,"completion_tokens":2}}`)
		writeSSE(w, flusher, "[DONE]")
	}))
	defer server.Close()

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	events, err := backend.Stream(context.Background(), inference.CompletionRequest{RequestID: "req-2", Model: "llama"})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var content string
	var last inference.StreamEvent
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("stream event error = %v", ev.Err)
		}
		content += ev.Content
		last = ev
	}

	if content != "hello" {
		t.Errorf("accumulated content = %q, want %q", content, "hello")
	}
	if !last.Done {
		t.Errorf("last event Done = false, want true")
	}
	if last.FinishReason != "stop" {
		t.Errorf("last event FinishReason = %q, want stop", last.FinishReason)
	}
	if last.Usage.PromptTokens != 3 || last.Usage.CompletionTokens != 2 {
		t.Errorf("last event Usage = %+v, want {3 2}", last.Usage)
	}
}

func TestVLLMBackend_CancelStopsAnInFlightStream(t *testing.T) {
	unblock := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		flusher := w.(http.Flusher)
		writeSSE(w, flusher, `{"choices":[{"index":0,"delta":{"content":"first"},"finish_reason":null}]}`)

		select {
		case <-unblock:
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	defer close(unblock)

	backend := inference.NewVLLMBackend(server.URL, server.Client())
	events, err := backend.Stream(context.Background(), inference.CompletionRequest{RequestID: "req-3", Model: "llama"})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	first := <-events
	if first.Content != "first" {
		t.Fatalf("first event content = %q, want %q", first.Content, "first")
	}

	if err := backend.Cancel(context.Background(), "req-3"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	select {
	case ev, ok := <-events:
		if ok && ev.Err == nil {
			t.Errorf("expected the stream to close or report an error after Cancel, got %+v", ev)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("stream did not close within 5s of Cancel")
	}
}
