package inference

import (
	"context"
	"testing"
	"time"
)

func testRequest(id string) CompletionRequest {
	return CompletionRequest{
		RequestID: id,
		Model:     "mock-model",
		Messages:  []Message{{Role: "user", Content: "hello world"}},
	}
}

func TestMockBackend_Complete_IsDeterministic(t *testing.T) {
	backend := NewMockBackend(0)
	ctx := context.Background()

	first, err := backend.Complete(ctx, testRequest("req-1"))
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	second, err := backend.Complete(ctx, testRequest("req-2"))
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}

	if first.Content != second.Content {
		t.Errorf("Content differs across calls: %q vs %q", first.Content, second.Content)
	}
	if first.FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", first.FinishReason)
	}
	if first.Usage.CompletionTokens == 0 {
		t.Error("CompletionTokens should be > 0")
	}
}

func TestMockBackend_Complete_RespectsMaxTokens(t *testing.T) {
	backend := NewMockBackend(0)
	req := testRequest("req-1")
	req.MaxTokens = 2

	resp, err := backend.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if resp.Usage.CompletionTokens != 2 {
		t.Errorf("CompletionTokens = %d, want 2", resp.Usage.CompletionTokens)
	}
}

func TestMockBackend_Complete_ContextCancelled(t *testing.T) {
	backend := NewMockBackend(50 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	if _, err := backend.Complete(ctx, testRequest("req-1")); err == nil {
		t.Fatal("Complete() error = nil, want context error")
	}
}

func TestMockBackend_Stream_EmitsTokensThenDone(t *testing.T) {
	backend := NewMockBackend(0)

	events, err := backend.Stream(context.Background(), testRequest("req-1"))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	var content string
	var sawDone bool
	for ev := range events {
		if ev.Err != nil {
			t.Fatalf("unexpected event error: %v", ev.Err)
		}
		if ev.Done {
			sawDone = true
			if ev.FinishReason != "stop" {
				t.Errorf("FinishReason = %q, want stop", ev.FinishReason)
			}
			continue
		}
		content += ev.Content
	}

	if !sawDone {
		t.Error("stream never emitted a Done event")
	}

	full, err := backend.Complete(context.Background(), testRequest("req-2"))
	if err != nil {
		t.Fatalf("Complete() error = %v", err)
	}
	if content != full.Content {
		t.Errorf("streamed content = %q, want %q (must match Complete)", content, full.Content)
	}
}

func TestMockBackend_Cancel_StopsStreamMidway(t *testing.T) {
	backend := NewMockBackend(20 * time.Millisecond)

	events, err := backend.Stream(context.Background(), testRequest("req-1"))
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}

	// Let the stream emit at least one token before cancelling.
	<-events

	if err := backend.Cancel(context.Background(), "req-1"); err != nil {
		t.Fatalf("Cancel() error = %v", err)
	}

	var sawCancelErr bool
	for ev := range events {
		if ev.Err != nil {
			sawCancelErr = true
		}
	}

	if !sawCancelErr {
		t.Error("stream should have terminated with an error after Cancel")
	}
}

func TestMockBackend_Cancel_UnknownRequestIsNoop(t *testing.T) {
	backend := NewMockBackend(0)
	if err := backend.Cancel(context.Background(), "never-started"); err != nil {
		t.Errorf("Cancel() error = %v, want nil for unknown request", err)
	}
}
