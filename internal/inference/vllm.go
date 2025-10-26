package inference

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
)

// VLLMBackend implements Backend against a live vLLM server's
// OpenAI-compatible chat completions endpoint
// (https://docs.vllm.ai/en/latest/serving/openai_compatible_server.html).
// It is the production implementation; MockBackend serves the same
// interface in CI, where no GPU is present to run a real engine.
type VLLMBackend struct {
	baseURL    string
	httpClient *http.Client

	mu       sync.Mutex
	inFlight map[string]context.CancelFunc
}

// NewVLLMBackend returns a VLLMBackend that talks to the vLLM server at
// baseURL (e.g. "http://127.0.0.1:8000") using httpClient. httpClient is
// caller-supplied rather than defaulted so request timeouts and
// connection pooling are configured once, centrally, alongside the
// gateway's other outbound clients.
func NewVLLMBackend(baseURL string, httpClient *http.Client) *VLLMBackend {
	return &VLLMBackend{
		baseURL:    strings.TrimRight(baseURL, "/"),
		httpClient: httpClient,
		inFlight:   make(map[string]context.CancelFunc),
	}
}

var _ Backend = (*VLLMBackend)(nil)

// vllmMessage mirrors the OpenAI chat message shape vLLM expects.
type vllmMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// vllmChatRequest mirrors the subset of vLLM's OpenAI-compatible request
// body Redline drives.
type vllmChatRequest struct {
	Model       string        `json:"model"`
	Messages    []vllmMessage `json:"messages"`
	Temperature float64       `json:"temperature,omitempty"`
	TopP        float64       `json:"top_p,omitempty"`
	MaxTokens   int           `json:"max_tokens,omitempty"`
	Stream      bool          `json:"stream"`
}

type vllmUsage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

type vllmChoice struct {
	Index        int          `json:"index"`
	Message      *vllmMessage `json:"message,omitempty"`
	Delta        *vllmMessage `json:"delta,omitempty"`
	FinishReason *string      `json:"finish_reason"`
}

// vllmChatResponse covers both the non-streaming response body and each
// individual streamed chunk: vLLM's SSE stream sends the same top-level
// shape per event, differing only in whether Choices[i].Message or
// Choices[i].Delta is populated.
type vllmChatResponse struct {
	Choices []vllmChoice `json:"choices"`
	Usage   *vllmUsage   `json:"usage,omitempty"`
}

func toVLLMRequest(req CompletionRequest, stream bool) vllmChatRequest {
	messages := make([]vllmMessage, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = vllmMessage(m)
	}
	return vllmChatRequest{
		Model:       req.Model,
		Messages:    messages,
		Temperature: req.Temperature,
		TopP:        req.TopP,
		MaxTokens:   req.MaxTokens,
		Stream:      stream,
	}
}

// Complete implements Backend.
func (v *VLLMBackend) Complete(ctx context.Context, req CompletionRequest) (*CompletionResponse, error) {
	body, err := json.Marshal(toVLLMRequest(req, false))
	if err != nil {
		return nil, fmt.Errorf("inference: marshal vllm request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, v.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("inference: build vllm request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("inference: vllm request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, newVLLMStatusError(resp.StatusCode, resp.Body)
	}

	var parsed vllmChatResponse
	if err := json.NewDecoder(resp.Body).Decode(&parsed); err != nil {
		return nil, fmt.Errorf("inference: decode vllm response: %w", err)
	}
	if len(parsed.Choices) == 0 || parsed.Choices[0].Message == nil {
		return nil, fmt.Errorf("inference: vllm response had no message choice")
	}

	choice := parsed.Choices[0]
	finishReason := ""
	if choice.FinishReason != nil {
		finishReason = *choice.FinishReason
	}
	usage := Usage{}
	if parsed.Usage != nil {
		usage = Usage{PromptTokens: parsed.Usage.PromptTokens, CompletionTokens: parsed.Usage.CompletionTokens}
	}

	return &CompletionResponse{
		RequestID:    req.RequestID,
		Model:        req.Model,
		Content:      choice.Message.Content,
		FinishReason: finishReason,
		Usage:        usage,
	}, nil
}

// Stream implements Backend, consuming vLLM's OpenAI-compatible SSE
// stream: each event is a line "data: <json>", terminated by a literal
// "data: [DONE]" line.
func (v *VLLMBackend) Stream(ctx context.Context, req CompletionRequest) (<-chan StreamEvent, error) {
	body, err := json.Marshal(toVLLMRequest(req, true))
	if err != nil {
		return nil, fmt.Errorf("inference: marshal vllm request: %w", err)
	}

	streamCtx, cancel := context.WithCancel(ctx)
	httpReq, err := http.NewRequestWithContext(streamCtx, http.MethodPost, v.baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("inference: build vllm request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("Accept", "text/event-stream")

	resp, err := v.httpClient.Do(httpReq)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("inference: vllm request: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		err := newVLLMStatusError(resp.StatusCode, resp.Body)
		_ = resp.Body.Close()
		cancel()
		return nil, err
	}

	v.register(req.RequestID, cancel)

	events := make(chan StreamEvent)
	go v.consumeStream(streamCtx, req, resp.Body, events, cancel)

	return events, nil
}

func (v *VLLMBackend) consumeStream(ctx context.Context, req CompletionRequest, body io.ReadCloser, events chan<- StreamEvent, cancel context.CancelFunc) {
	defer close(events)
	defer func() { _ = body.Close() }()
	defer cancel()
	defer v.unregister(req.RequestID)

	var content strings.Builder
	var finishReason string
	var usage Usage

	scanner := bufio.NewScanner(body)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}

		var chunk vllmChatResponse
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			events <- StreamEvent{RequestID: req.RequestID, Err: fmt.Errorf("inference: decode vllm stream chunk: %w", err)}
			return
		}
		if chunk.Usage != nil {
			usage = Usage{PromptTokens: chunk.Usage.PromptTokens, CompletionTokens: chunk.Usage.CompletionTokens}
		}
		if len(chunk.Choices) == 0 {
			continue
		}
		choice := chunk.Choices[0]
		if choice.FinishReason != nil {
			finishReason = *choice.FinishReason
		}
		if choice.Delta != nil && choice.Delta.Content != "" {
			content.WriteString(choice.Delta.Content)
			events <- StreamEvent{RequestID: req.RequestID, Content: choice.Delta.Content}
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			events <- StreamEvent{RequestID: req.RequestID, Err: ErrCancelled}
			return
		}
		events <- StreamEvent{RequestID: req.RequestID, Err: fmt.Errorf("inference: read vllm stream: %w", err)}
		return
	}

	events <- StreamEvent{
		RequestID:    req.RequestID,
		Done:         true,
		FinishReason: finishReason,
		Usage:        usage,
	}
}

// Cancel implements Backend by cancelling the HTTP request context
// backing the matching in-flight Stream, if any. Cancelling an unknown or
// already-finished request id is a no-op, not an error.
func (v *VLLMBackend) Cancel(_ context.Context, requestID string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	if cancel, ok := v.inFlight[requestID]; ok {
		cancel()
		delete(v.inFlight, requestID)
	}
	return nil
}

func (v *VLLMBackend) register(requestID string, cancel context.CancelFunc) {
	v.mu.Lock()
	defer v.mu.Unlock()
	v.inFlight[requestID] = cancel
}

func (v *VLLMBackend) unregister(requestID string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	delete(v.inFlight, requestID)
}

// maxErrorBodyBytes bounds how much of a non-200 response body
// newVLLMStatusError reads into the returned error: enough to carry a
// CUDA OOM stack trace's identifying line for IsCUDAOutOfMemory to
// classify, without risking an unbounded read against a misbehaving
// upstream.
const maxErrorBodyBytes = 4096

// newVLLMStatusError builds the error returned for a non-200 vLLM
// response, folding in the response body: IsCUDAOutOfMemory needs the
// body's text to distinguish a CUDA out-of-memory failure from any other
// server error, since vLLM reports both as a plain 500.
func newVLLMStatusError(statusCode int, body io.Reader) error {
	snippet, _ := io.ReadAll(io.LimitReader(body, maxErrorBodyBytes))
	return fmt.Errorf("inference: vllm returned status %d: %s", statusCode, snippet)
}
