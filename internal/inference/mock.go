package inference

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrCancelled is the error carried by a StreamEvent when Cancel stopped
// the stream before it finished naturally.
var ErrCancelled = errors.New("inference: cancelled")

// MockBackend is a deterministic Backend implementation: the same request
// always produces the same token sequence, with a configurable delay
// between tokens. It requires no GPU, so the full request path - auth,
// rate limiting, validation, streaming - is exercisable in CI.
type MockBackend struct {
	tokenDelay time.Duration

	mu       sync.Mutex
	inFlight map[string]chan struct{}
}

// NewMockBackend returns a MockBackend that waits tokenDelay between
// emitting each token of a streamed response.
func NewMockBackend(tokenDelay time.Duration) *MockBackend {
	return &MockBackend{
		tokenDelay: tokenDelay,
		inFlight:   make(map[string]chan struct{}),
	}
}

var _ Backend = (*MockBackend)(nil)

// Complete implements Backend.
func (m *MockBackend) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	tokens := m.tokens(req)

	for range tokens {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(m.tokenDelay):
		}
	}

	return &CompletionResponse{
		RequestID:    req.RequestID,
		Model:        req.Model,
		Content:      strings.Join(tokens, ""),
		FinishReason: "stop",
		Usage:        Usage{PromptTokens: promptTokenCount(req), CompletionTokens: len(tokens)},
	}, nil
}

// Stream implements Backend.
func (m *MockBackend) Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error) {
	tokens := m.tokens(req)
	events := make(chan StreamEvent)
	cancel := m.register(req.RequestID)

	go func() {
		defer close(events)
		defer m.unregister(req.RequestID)

		for _, tok := range tokens {
			select {
			case <-ctx.Done():
				events <- StreamEvent{RequestID: req.RequestID, Err: ctx.Err()}
				return
			case <-cancel:
				events <- StreamEvent{RequestID: req.RequestID, Err: ErrCancelled}
				return
			case <-time.After(m.tokenDelay):
			}
			events <- StreamEvent{RequestID: req.RequestID, Content: tok}
		}

		events <- StreamEvent{
			RequestID:    req.RequestID,
			Done:         true,
			FinishReason: "stop",
			Usage:        Usage{PromptTokens: promptTokenCount(req), CompletionTokens: len(tokens)},
		}
	}()

	return events, nil
}

// Cancel implements Backend. Cancelling an unknown or already-finished
// request id is a no-op, not an error.
func (m *MockBackend) Cancel(_ context.Context, requestID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	if cancel, ok := m.inFlight[requestID]; ok {
		close(cancel)
		delete(m.inFlight, requestID)
	}
	return nil
}

func (m *MockBackend) register(requestID string) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()

	cancel := make(chan struct{})
	m.inFlight[requestID] = cancel
	return cancel
}

func (m *MockBackend) unregister(requestID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.inFlight, requestID)
}

// tokens deterministically derives a token sequence from req: same
// messages and model in, same tokens out, every time.
func (m *MockBackend) tokens(req CompletionRequest) []string {
	last := ""
	if len(req.Messages) > 0 {
		last = req.Messages[len(req.Messages)-1].Content
	}

	words := strings.Fields(fmt.Sprintf("mock response to: %s", last))
	tokens := make([]string, len(words))
	for i, w := range words {
		if i > 0 {
			tokens[i] = " " + w
		} else {
			tokens[i] = w
		}
	}

	if req.MaxTokens > 0 && len(tokens) > req.MaxTokens {
		tokens = tokens[:req.MaxTokens]
	}

	return tokens
}

func promptTokenCount(req CompletionRequest) int {
	n := 0
	for _, msg := range req.Messages {
		n += len(strings.Fields(msg.Content))
	}
	return n
}
