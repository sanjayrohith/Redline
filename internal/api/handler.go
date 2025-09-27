package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/inference"
)

// ChatCompletionsHandler wires the OpenAI-compatible non-streaming
// /v1/chat/completions endpoint against backend: decode, validate, run
// the completion, and render a spec-shaped response. Authentication and
// rate limiting are applied by middleware around this handler, not here.
func ChatCompletionsHandler(backend inference.Backend) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		requestID, _ := httpmw.RequestIDFromContext(r.Context())

		var req ChatCompletionRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			apierror.Write(w, http.StatusBadRequest, apierror.CodeValidation, "malformed request body", requestID)
			return
		}

		if err := req.Validate(); err != nil {
			apierror.Write(w, http.StatusUnprocessableEntity, apierror.CodeValidation, err.Error(), requestID)
			return
		}

		result, err := backend.Complete(r.Context(), toBackendRequest(requestID, req))
		if err != nil {
			apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "completion failed", requestID)
			return
		}

		resp := ChatCompletionResponse{
			ID:      "chatcmpl-" + requestID,
			Object:  "chat.completion",
			Created: time.Now().Unix(),
			Model:   result.Model,
			Choices: []ChatCompletionChoice{{
				Index:        0,
				Message:      ChatMessage{Role: "assistant", Content: result.Content},
				FinishReason: result.FinishReason,
			}},
			Usage: ChatCompletionUsage{
				PromptTokens:     result.Usage.PromptTokens,
				CompletionTokens: result.Usage.CompletionTokens,
				TotalTokens:      result.Usage.PromptTokens + result.Usage.CompletionTokens,
			},
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(resp)
	}
}

func toBackendRequest(requestID string, req ChatCompletionRequest) inference.CompletionRequest {
	messages := make([]inference.Message, len(req.Messages))
	for i, m := range req.Messages {
		messages[i] = inference.Message{Role: m.Role, Content: m.Content}
	}

	backendReq := inference.CompletionRequest{
		RequestID: requestID,
		Model:     req.Model,
		Messages:  messages,
	}
	if req.Temperature != nil {
		backendReq.Temperature = *req.Temperature
	}
	if req.TopP != nil {
		backendReq.TopP = *req.TopP
	}
	if req.MaxTokens != nil {
		backendReq.MaxTokens = *req.MaxTokens
	}

	return backendReq
}
