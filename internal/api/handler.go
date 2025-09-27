package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/inference"
)

// ChatCompletionsLimits bounds the resources one completion request may
// consume: the total sequence length (prompt plus requested completion
// tokens) accepted before dispatch, and the hard wall-clock deadline
// placed on the generation call itself.
type ChatCompletionsLimits struct {
	MaxSequenceLength int
	GenerationTimeout time.Duration
}

// ChatCompletionsHandler wires the OpenAI-compatible non-streaming
// /v1/chat/completions endpoint against backend: decode, validate, enforce
// limits, run the completion under a hard deadline, and render a
// spec-shaped response. Authentication and rate limiting are applied by
// middleware around this handler, not here.
func ChatCompletionsHandler(backend inference.Backend, limits ChatCompletionsLimits) http.HandlerFunc {
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

		if limits.MaxSequenceLength > 0 {
			if sequenceLength := estimatedSequenceLength(req); sequenceLength > limits.MaxSequenceLength {
				apierror.Write(w, http.StatusUnprocessableEntity, apierror.CodeValidation,
					fmt.Sprintf("request sequence length %d exceeds the maximum of %d tokens", sequenceLength, limits.MaxSequenceLength),
					requestID)
				return
			}
		}

		genCtx := r.Context()
		if limits.GenerationTimeout > 0 {
			var cancel context.CancelFunc
			genCtx, cancel = context.WithTimeout(genCtx, limits.GenerationTimeout)
			defer cancel()
		}

		result, err := backend.Complete(genCtx, toBackendRequest(requestID, req))
		if err != nil {
			if errors.Is(err, context.DeadlineExceeded) {
				apierror.Write(w, http.StatusGatewayTimeout, apierror.CodeTimeout, "generation timed out", requestID)
				return
			}
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

// estimatedSequenceLength approximates the total context window a request
// will consume - prompt words plus any requested completion tokens - using
// a cheap word-count heuristic. It is deliberately conservative rather
// than exact, since its job is to reject a request before dispatch, not to
// bill it.
func estimatedSequenceLength(req ChatCompletionRequest) int {
	n := 0
	for _, m := range req.Messages {
		n += len(strings.Fields(m.Content))
	}
	if req.MaxTokens != nil {
		n += *req.MaxTokens
	}
	return n
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
