package inference

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"
)

// ErrEngineNotReady means the vLLM server has not finished warming up:
// either its HTTP listener is not answering /health yet, or /health
// answers but a real generation request still fails or times out -
// which happens while weights are still loading or CUDA graphs are
// still compiling, well after the listener itself comes up.
var ErrEngineNotReady = errors.New("inference: vllm engine not ready")

// ReadinessProbe reports whether a vLLM server is actually ready to
// serve traffic, not merely that its process is running. vLLM's /health
// endpoint reports the HTTP server is up; it does not distinguish "still
// loading weights" or "still compiling CUDA graphs" from "ready to
// generate". A minimal real completion is the only way to observe that
// distinction from outside the process, so Check always issues one.
type ReadinessProbe struct {
	backend    *VLLMBackend
	httpClient *http.Client
	baseURL    string
	model      string
}

// NewReadinessProbe returns a ReadinessProbe against the vLLM server at
// baseURL, warming up model.
func NewReadinessProbe(baseURL, model string, httpClient *http.Client) *ReadinessProbe {
	return &ReadinessProbe{
		backend:    NewVLLMBackend(baseURL, httpClient),
		httpClient: httpClient,
		baseURL:    baseURL,
		model:      model,
	}
}

// Check performs one readiness attempt: the HTTP health endpoint must
// answer 200, and a minimal completion (a single output token) must
// succeed, proving the engine has weights loaded and can actually
// generate - not just that its listener accepts connections.
func (p *ReadinessProbe) Check(ctx context.Context) error {
	healthReq, err := http.NewRequestWithContext(ctx, http.MethodGet, p.baseURL+"/health", nil)
	if err != nil {
		return fmt.Errorf("inference: build vllm health request: %w", err)
	}
	healthResp, err := p.httpClient.Do(healthReq)
	if err != nil {
		return fmt.Errorf("%w: health check failed: %v", ErrEngineNotReady, err)
	}
	_ = healthResp.Body.Close()
	if healthResp.StatusCode != http.StatusOK {
		return fmt.Errorf("%w: health endpoint returned status %d", ErrEngineNotReady, healthResp.StatusCode)
	}

	_, err = p.backend.Complete(ctx, CompletionRequest{
		RequestID: "readiness-probe",
		Model:     p.model,
		Messages:  []Message{{Role: "user", Content: "ping"}},
		MaxTokens: 1,
	})
	if err != nil {
		return fmt.Errorf("%w: warmup completion failed: %v", ErrEngineNotReady, err)
	}
	return nil
}

// WaitReady polls Check every pollInterval until it succeeds or ctx is
// done, so a deployment withholds traffic until the engine has
// demonstrably finished loading and compiling, not just started.
func (p *ReadinessProbe) WaitReady(ctx context.Context, pollInterval time.Duration) error {
	for {
		err := p.Check(ctx)
		if err == nil {
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("%w: %v (last attempt: %v)", ErrEngineNotReady, ctx.Err(), err)
		case <-time.After(pollInterval):
		}
	}
}
