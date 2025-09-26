// Package inference declares the gateway's provider-agnostic inference
// backend interface, written once against this abstraction so the same
// request path serves a mock backend in tests and vLLM in production.
package inference

import "context"

// Message is one turn in a chat completion request.
type Message struct {
	Role    string
	Content string
}

// CompletionRequest is a backend-agnostic chat completion request.
// RequestID is caller-supplied and is the handle Cancel later uses to stop
// a matching in-flight Stream.
type CompletionRequest struct {
	RequestID   string
	Model       string
	Messages    []Message
	Temperature float64
	TopP        float64
	MaxTokens   int
}

// Usage is the token accounting for one completion.
type Usage struct {
	PromptTokens     int
	CompletionTokens int
}

// CompletionResponse is a backend-agnostic non-streaming completion result.
type CompletionResponse struct {
	RequestID    string
	Model        string
	Content      string
	FinishReason string
	Usage        Usage
}

// StreamEvent is one increment of a streamed completion. A backend sends
// zero or more events with Done false, each carrying the next content
// chunk, followed by exactly one event with Done true carrying the final
// FinishReason and Usage. Err is set, and the stream closed, if generation
// fails before completion.
type StreamEvent struct {
	RequestID    string
	Content      string
	Done         bool
	FinishReason string
	Usage        Usage
	Err          error
}

// Backend is the provider-agnostic inference surface the gateway is
// written against. A mock implementation exercises the full request path
// in CI with no GPU present; a vLLM implementation serves production
// traffic behind the same interface.
type Backend interface {
	// Complete runs req to completion and returns the full response.
	Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error)

	// Stream runs req and returns a channel of incremental StreamEvents,
	// terminated by exactly one Done event or a closed channel on error.
	Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error)

	// Cancel stops the in-flight Stream identified by requestID, if any.
	// Cancelling a request that is not running, or has already finished,
	// is not an error.
	Cancel(ctx context.Context, requestID string) error
}
