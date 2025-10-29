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

	// TTFT, if non-nil, records each streamed request's admission-to-
	// first-token latency. Nil means "don't record" - a deployment with
	// no metrics registry wired up still serves traffic correctly.
	TTFT TTFTRecorder
	// TPOT, if non-nil, records each gap between consecutive tokens
	// during a streamed request's decode phase.
	TPOT TPOTRecorder
	// Quantization labels every TTFT observation this handler records:
	// the precision this deployment's resident engine actually runs at,
	// fixed for the handler's lifetime rather than per-request, since a
	// single backend instance serves one deployment at one precision.
	Quantization string
}

// TTFTRecorder records one request's observed time-to-first-token
// latency, labeled by model and quantization. metrics.InferenceCollectors
// implements this; declared here, narrow to just what this package
// needs, so api does not import the metrics/prometheus stack directly.
type TTFTRecorder interface {
	ObserveTimeToFirstToken(model, quantization string, d time.Duration)
}

// TPOTRecorder records one observed gap between two consecutive tokens
// of the same streamed response's decode phase, labeled by model and
// quantization. metrics.InferenceCollectors implements this.
type TPOTRecorder interface {
	ObserveInterTokenLatency(model, quantization string, d time.Duration)
}

// ChatCompletionsHandler wires the OpenAI-compatible non-streaming
// /v1/chat/completions endpoint against backend: decode, validate, enforce
// limits, run the completion under a hard deadline, and render a
// spec-shaped response. Authentication and rate limiting are applied by
// middleware around this handler, not here.
func ChatCompletionsHandler(backend inference.Backend, limits ChatCompletionsLimits) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		admittedAt := time.Now()
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

		if req.Stream {
			streamChatCompletion(genCtx, w, backend, requestID, req, admittedAt, limits.TTFT, limits.TPOT, limits.Quantization)
			return
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

// streamChatCompletion drives an OpenAI-compatible SSE response off
// backend.Stream: one "data: <chunk json>\n\n" event per token, flushed
// immediately so the client sees each token as it arrives rather than
// buffered behind Go's default response buffering, a final chunk
// carrying the finish reason and usage, and the SSE-standard
// "data: [DONE]\n\n" terminator.
//
// Response headers are already committed (status 200, event-stream
// content type) by the time any error can occur, since a mid-stream
// generation failure has no HTTP status left to report through - the
// stream simply ends without its normal chunks, which is the same
// failure signature a client sees for a plain dropped connection.
func streamChatCompletion(ctx context.Context, w http.ResponseWriter, backend inference.Backend, requestID string, req ChatCompletionRequest, admittedAt time.Time, ttft TTFTRecorder, tpot TPOTRecorder, quantization string) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "streaming not supported", requestID)
		return
	}

	events, err := backend.Stream(ctx, toBackendRequest(requestID, req))
	if err != nil {
		apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "completion failed", requestID)
		return
	}

	// ctx is cancelled the moment the client disconnects (net/http cancels
	// a server request's context as soon as the underlying connection
	// closes) as well as when this handler returns normally. Either way,
	// telling the backend to cancel explicitly - rather than trusting
	// every Backend implementation to notice ctx cancellation deep inside
	// its own Stream loop - is what actually stops an abandoned request
	// from continuing to consume GPU cycles once nothing is reading its
	// output anymore. A Cancel call after the request already finished on
	// its own is a defined no-op, so firing it unconditionally here is safe.
	go func() { // #nosec G118 -- context.Background is deliberate: ctx is already Done by the time Cancel is called, so it cannot be reused
		<-ctx.Done()
		_ = backend.Cancel(context.Background(), requestID)
	}()

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)

	id := "chatcmpl-" + requestID
	created := time.Now().Unix()
	first := true
	ttftRecorded := false
	var lastTokenAt time.Time

	for ev := range events {
		if ev.Err != nil {
			return
		}

		if !ev.Done {
			delta := ChatCompletionChunkDelta{Content: ev.Content}
			if first {
				delta.Role = "assistant"
				first = false
			}
			if ev.Content != "" {
				now := time.Now()
				if !ttftRecorded {
					if ttft != nil {
						ttft.ObserveTimeToFirstToken(req.Model, quantization, now.Sub(admittedAt))
					}
					ttftRecorded = true
				} else if tpot != nil {
					tpot.ObserveInterTokenLatency(req.Model, quantization, now.Sub(lastTokenAt))
				}
				lastTokenAt = now
			}
			writeSSEChunk(w, flusher, ChatCompletionChunk{
				ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
				Choices: []ChatCompletionChunkChoice{{Index: 0, Delta: delta}},
			})
			continue
		}

		finishReason := ev.FinishReason
		writeSSEChunk(w, flusher, ChatCompletionChunk{
			ID: id, Object: "chat.completion.chunk", Created: created, Model: req.Model,
			Choices: []ChatCompletionChunkChoice{{Index: 0, Delta: ChatCompletionChunkDelta{}, FinishReason: &finishReason}},
			Usage: &ChatCompletionUsage{
				PromptTokens:     ev.Usage.PromptTokens,
				CompletionTokens: ev.Usage.CompletionTokens,
				TotalTokens:      ev.Usage.PromptTokens + ev.Usage.CompletionTokens,
			},
		})
	}

	_, _ = fmt.Fprint(w, "data: [DONE]\n\n")
	flusher.Flush()
}

func writeSSEChunk(w http.ResponseWriter, flusher http.Flusher, chunk ChatCompletionChunk) {
	payload, err := json.Marshal(chunk)
	if err != nil {
		return
	}
	_, _ = fmt.Fprintf(w, "data: %s\n\n", payload)
	flusher.Flush()
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
