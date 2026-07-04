package api

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/inference"
)

// sseDataLines extracts the payload of every "data: ..." line from a raw
// SSE response body, in order.
func sseDataLines(t *testing.T, body string) []string {
	t.Helper()
	var lines []string
	scanner := bufio.NewScanner(strings.NewReader(body))
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "data: ") {
			lines = append(lines, strings.TrimPrefix(line, "data: "))
		}
	}
	return lines
}

type spyWatchdog struct {
	touched []string
}

func (s *spyWatchdog) Touch(requestID string) {
	s.touched = append(s.touched, requestID)
}

func TestChatCompletionsHandler_Stream_TouchesWatchdogOnEveryToken(t *testing.T) {
	backend := inference.NewMockBackend(0)
	watchdog := &spyWatchdog{}
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{Watchdog: watchdog})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi there"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if len(watchdog.touched) == 0 {
		t.Fatal("watchdog was never touched during a streamed response with content")
	}
}

func TestChatCompletionsHandler_Stream_EmitsChunksThenDone(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi there"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q, want text/event-stream", ct)
	}

	lines := sseDataLines(t, rec.Body.String())
	if len(lines) < 2 {
		t.Fatalf("got %d SSE data lines, want at least 2 (one content chunk + [DONE])", len(lines))
	}
	if lines[len(lines)-1] != "[DONE]" {
		t.Errorf("last SSE data line = %q, want [DONE]", lines[len(lines)-1])
	}

	var sawContent bool
	var final *ChatCompletionChunk
	for _, line := range lines[:len(lines)-1] {
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Fatalf("unmarshal chunk %q: %v", line, err)
		}
		if len(chunk.Choices) != 1 {
			t.Fatalf("chunk has %d choices, want 1", len(chunk.Choices))
		}
		if chunk.Choices[0].Delta.Content != "" {
			sawContent = true
		}
		if chunk.Choices[0].FinishReason != nil {
			c := chunk
			final = &c
		}
	}

	if !sawContent {
		t.Error("no chunk carried any content")
	}
	if final == nil {
		t.Fatal("no chunk carried a finish_reason")
	}
	if *final.Choices[0].FinishReason != "stop" {
		t.Errorf("final finish_reason = %q, want stop", *final.Choices[0].FinishReason)
	}
	if final.Usage == nil {
		t.Fatal("final chunk carried no usage")
	}
	if final.Usage.TotalTokens != final.Usage.PromptTokens+final.Usage.CompletionTokens {
		t.Errorf("Usage.TotalTokens = %d, want PromptTokens+CompletionTokens", final.Usage.TotalTokens)
	}
}

func TestChatCompletionsHandler_Stream_FirstChunkCarriesRole(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler(rec, req)

	lines := sseDataLines(t, rec.Body.String())
	var first ChatCompletionChunk
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal first chunk: %v", err)
	}
	if first.Choices[0].Delta.Role != "assistant" {
		t.Errorf("first chunk Delta.Role = %q, want assistant", first.Choices[0].Delta.Role)
	}
}

func TestChatCompletionsHandler_Stream_ReconstructedContentMatchesNonStreaming(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	nonStreamBody := `{"model":"mock-model","messages":[{"role":"user","content":"assemble me"}]}`
	nonStreamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(nonStreamBody))
	nonStreamRec := httptest.NewRecorder()
	handler(nonStreamRec, nonStreamReq)

	var full ChatCompletionResponse
	if err := json.Unmarshal(nonStreamRec.Body.Bytes(), &full); err != nil {
		t.Fatalf("unmarshal non-streaming response: %v", err)
	}

	streamBody := `{"model":"mock-model","messages":[{"role":"user","content":"assemble me"}],"stream":true}`
	streamReq := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(streamBody))
	streamRec := httptest.NewRecorder()
	handler(streamRec, streamReq)

	lines := sseDataLines(t, streamRec.Body.String())
	var reconstructed strings.Builder
	for _, line := range lines[:len(lines)-1] {
		var chunk ChatCompletionChunk
		if err := json.Unmarshal([]byte(line), &chunk); err != nil {
			t.Fatalf("unmarshal chunk %q: %v", line, err)
		}
		reconstructed.WriteString(chunk.Choices[0].Delta.Content)
	}

	if reconstructed.String() != full.Choices[0].Message.Content {
		t.Errorf("reconstructed streamed content = %q, want it to match the non-streaming response %q",
			reconstructed.String(), full.Choices[0].Message.Content)
	}
}

func TestChatCompletionsHandler_Stream_GenerationErrorEndsStreamWithoutDone(t *testing.T) {
	backend := &erroringStreamBackend{}
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (headers are already committed before a stream-time error)", rec.Code)
	}
	lines := sseDataLines(t, rec.Body.String())
	for _, line := range lines {
		if line == "[DONE]" {
			t.Error("stream sent [DONE] despite a generation error; a client cannot distinguish a truncated failure from a clean finish")
		}
	}
}

// erroringStreamBackend is a Backend whose Stream immediately emits an
// error event, simulating a generation failure partway through.
type erroringStreamBackend struct{}

var _ inference.Backend = erroringStreamBackend{}

func (erroringStreamBackend) Complete(context.Context, inference.CompletionRequest) (*inference.CompletionResponse, error) {
	return nil, errors.New("not used by this test")
}

func (erroringStreamBackend) Stream(_ context.Context, req inference.CompletionRequest) (<-chan inference.StreamEvent, error) {
	events := make(chan inference.StreamEvent, 1)
	events <- inference.StreamEvent{RequestID: req.RequestID, Err: errors.New("generation failed")}
	close(events)
	return events, nil
}

func (erroringStreamBackend) Cancel(context.Context, string) error {
	return nil
}
