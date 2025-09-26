// Package api declares the gateway's OpenAI-compatible request and
// response types.
package api

import (
	"fmt"
	"strings"
)

// ChatMessage is one turn of a chat completion request or response.
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatCompletionRequest is the OpenAI-compatible /v1/chat/completions
// request body. Optional numeric fields are pointers so "not provided" is
// distinguishable from an explicit zero value.
type ChatCompletionRequest struct {
	Model       string        `json:"model"`
	Messages    []ChatMessage `json:"messages"`
	Temperature *float64      `json:"temperature,omitempty"`
	TopP        *float64      `json:"top_p,omitempty"`
	MaxTokens   *int          `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream,omitempty"`
}

var validRoles = map[string]bool{"system": true, "user": true, "assistant": true}

// FieldError is one field-level validation failure.
type FieldError struct {
	Field   string
	Message string
}

// ValidationError aggregates every FieldError found on one request, so a
// client sees all problems at once rather than one at a time.
type ValidationError struct {
	Errors []FieldError
}

func (e *ValidationError) Error() string {
	parts := make([]string, len(e.Errors))
	for i, fe := range e.Errors {
		parts[i] = fmt.Sprintf("%s: %s", fe.Field, fe.Message)
	}
	return strings.Join(parts, "; ")
}

// Validate checks model, messages, temperature, top_p, max_tokens against
// the OpenAI-compatible constraints, returning a *ValidationError
// aggregating every violation, or nil if the request is well-formed.
func (r ChatCompletionRequest) Validate() error {
	var errs []FieldError

	if strings.TrimSpace(r.Model) == "" {
		errs = append(errs, FieldError{"model", "must not be empty"})
	}

	if len(r.Messages) == 0 {
		errs = append(errs, FieldError{"messages", "must contain at least one message"})
	}
	for i, m := range r.Messages {
		if !validRoles[m.Role] {
			errs = append(errs, FieldError{fmt.Sprintf("messages[%d].role", i), "must be one of system, user, assistant"})
		}
		if strings.TrimSpace(m.Content) == "" {
			errs = append(errs, FieldError{fmt.Sprintf("messages[%d].content", i), "must not be empty"})
		}
	}

	if r.Temperature != nil && (*r.Temperature < 0 || *r.Temperature > 2) {
		errs = append(errs, FieldError{"temperature", "must be between 0 and 2"})
	}
	if r.TopP != nil && (*r.TopP <= 0 || *r.TopP > 1) {
		errs = append(errs, FieldError{"top_p", "must be greater than 0 and at most 1"})
	}
	if r.MaxTokens != nil && *r.MaxTokens <= 0 {
		errs = append(errs, FieldError{"max_tokens", "must be greater than 0"})
	}

	if len(errs) > 0 {
		return &ValidationError{Errors: errs}
	}
	return nil
}

// ChatCompletionUsage is the OpenAI-compatible token usage block.
type ChatCompletionUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// ChatCompletionChoice is one generated completion in a response.
type ChatCompletionChoice struct {
	Index        int         `json:"index"`
	Message      ChatMessage `json:"message"`
	FinishReason string      `json:"finish_reason"`
}

// ChatCompletionResponse is the OpenAI-compatible /v1/chat/completions
// response body.
type ChatCompletionResponse struct {
	ID      string                 `json:"id"`
	Object  string                 `json:"object"`
	Created int64                  `json:"created"`
	Model   string                 `json:"model"`
	Choices []ChatCompletionChoice `json:"choices"`
	Usage   ChatCompletionUsage    `json:"usage"`
}
