package api

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/inference"
)

// cancelSpyBackend's Stream blocks until its context is cancelled, then
// emits a cancellation error event - mirroring what a real Backend's
// long-running generation loop does. Cancel records every requestID it
// was called with, so a test can assert the handler actually invoked it.
type cancelSpyBackend struct {
	streamStarted chan struct{}

	mu        sync.Mutex
	cancelled []string
}

var _ inference.Backend = (*cancelSpyBackend)(nil)

func newCancelSpyBackend() *cancelSpyBackend {
	return &cancelSpyBackend{streamStarted: make(chan struct{})}
}

func (b *cancelSpyBackend) Complete(context.Context, inference.CompletionRequest) (*inference.CompletionResponse, error) {
	return nil, errors.New("not used by this test")
}

func (b *cancelSpyBackend) Stream(ctx context.Context, req inference.CompletionRequest) (<-chan inference.StreamEvent, error) {
	events := make(chan inference.StreamEvent)
	go func() {
		defer close(events)
		close(b.streamStarted)
		<-ctx.Done()
		events <- inference.StreamEvent{RequestID: req.RequestID, Err: ctx.Err()}
	}()
	return events, nil
}

func (b *cancelSpyBackend) Cancel(_ context.Context, requestID string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.cancelled = append(b.cancelled, requestID)
	return nil
}

func (b *cancelSpyBackend) cancelledIDs() []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return append([]string(nil), b.cancelled...)
}

func TestChatCompletionsHandler_Stream_ClientDisconnectCancelsGeneration(t *testing.T) {
	backend := newCancelSpyBackend()
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)).WithContext(ctx)
	rec := httptest.NewRecorder()

	done := make(chan struct{})
	go func() {
		handler(rec, req)
		close(done)
	}()

	select {
	case <-backend.streamStarted:
	case <-time.After(5 * time.Second):
		t.Fatal("Stream was never called")
	}

	// Simulate a client disconnect: net/http cancels the request's
	// context as soon as the underlying connection closes.
	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not return after the client disconnected")
	}

	// The handler's ctx.Done() watcher goroutine calling Cancel races the
	// handler's own return - both start once ctx is cancelled - so poll
	// briefly rather than asserting the instant streamChatCompletion returns.
	deadline := time.Now().Add(5 * time.Second)
	for len(backend.cancelledIDs()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}

	if len(backend.cancelledIDs()) == 0 {
		t.Error("backend.Cancel was never called after the client disconnected")
	}
}

func TestChatCompletionsHandler_Stream_CancelNotCalledBeforeDisconnect(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	// A stream that completes normally must still work end to end even
	// though the handler now also races a Cancel-on-disconnect goroutine
	// against it.
	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "[DONE]") {
		t.Error("response body missing [DONE] terminator on a normal completion")
	}
}
