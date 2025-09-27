package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestChatCompletionsHandler_Success(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hello"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if resp.Object != "chat.completion" {
		t.Errorf("Object = %q, want chat.completion", resp.Object)
	}
	if resp.Model != "mock-model" {
		t.Errorf("Model = %q, want mock-model", resp.Model)
	}
	if len(resp.Choices) != 1 {
		t.Fatalf("len(Choices) = %d, want 1", len(resp.Choices))
	}
	if resp.Choices[0].Message.Role != "assistant" {
		t.Errorf("Choices[0].Message.Role = %q, want assistant", resp.Choices[0].Message.Role)
	}
	if resp.Choices[0].FinishReason != "stop" {
		t.Errorf("FinishReason = %q, want stop", resp.Choices[0].FinishReason)
	}
	if resp.Usage.TotalTokens != resp.Usage.PromptTokens+resp.Usage.CompletionTokens {
		t.Error("TotalTokens should equal prompt + completion tokens")
	}
}

func TestChatCompletionsHandler_MalformedJSON(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", bytes.NewBufferString("{not json"))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestChatCompletionsHandler_ValidationFailure(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"","messages":[]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "validation_error" {
		t.Errorf("error.code = %q, want validation_error", env.Error.Code)
	}
}

func TestChatCompletionsHandler_RespectsMaxTokens(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"a longer message with many words"}],"max_tokens":2}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}

	var resp ChatCompletionResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Usage.CompletionTokens != 2 {
		t.Errorf("CompletionTokens = %d, want 2", resp.Usage.CompletionTokens)
	}
}

func TestChatCompletionsHandler_RejectsSequenceOverMaxLength(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{MaxSequenceLength: 5})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"one two three four five six seven"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422, body = %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "validation_error" {
		t.Errorf("error.code = %q, want validation_error", env.Error.Code)
	}
}

func TestChatCompletionsHandler_AllowsSequenceAtMaxLength(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{MaxSequenceLength: 2})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
}

func TestChatCompletionsHandler_GenerationTimeout(t *testing.T) {
	backend := inference.NewMockBackend(50 * time.Millisecond)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{GenerationTimeout: 10 * time.Millisecond})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"this needs several tokens to generate"}]}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusGatewayTimeout {
		t.Fatalf("status = %d, want 504, body = %s", rec.Code, rec.Body.String())
	}

	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Error.Code != "request_timeout" {
		t.Errorf("error.code = %q, want request_timeout", env.Error.Code)
	}
}
