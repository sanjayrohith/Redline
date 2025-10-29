package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/inference"
)

// spyTTFTRecorder records every ObserveTimeToFirstToken call it receives.
type spyTTFTRecorder struct {
	mu           sync.Mutex
	observations []spyTTFTObservation
}

type spyTTFTObservation struct {
	model        string
	quantization string
	duration     time.Duration
}

func (s *spyTTFTRecorder) ObserveTimeToFirstToken(model, quantization string, d time.Duration) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.observations = append(s.observations, spyTTFTObservation{model: model, quantization: quantization, duration: d})
}

func (s *spyTTFTRecorder) all() []spyTTFTObservation {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]spyTTFTObservation(nil), s.observations...)
}

func TestChatCompletionsHandler_Stream_RecordsTimeToFirstToken(t *testing.T) {
	recorder := &spyTTFTRecorder{}
	backend := inference.NewMockBackend(10 * time.Millisecond)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{TTFT: recorder, Quantization: "fp8"})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi there"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler(rec, req)

	observations := recorder.all()
	if len(observations) != 1 {
		t.Fatalf("got %d TTFT observations, want exactly 1", len(observations))
	}
	obs := observations[0]
	if obs.model != "mock-model" {
		t.Errorf("model = %q, want mock-model", obs.model)
	}
	if obs.quantization != "fp8" {
		t.Errorf("quantization = %q, want fp8", obs.quantization)
	}
	if obs.duration <= 0 {
		t.Errorf("duration = %v, want > 0", obs.duration)
	}
	// The mock backend waits 10ms before its first token; the recorded
	// TTFT must reflect that, not read as ~0 from measuring the wrong point.
	if obs.duration < 5*time.Millisecond {
		t.Errorf("duration = %v, want at least ~10ms given the mock backend's token delay", obs.duration)
	}
}

func TestChatCompletionsHandler_Stream_NilTTFTRecorderIsSafe(t *testing.T) {
	backend := inference.NewMockBackend(0)
	handler := ChatCompletionsHandler(backend, ChatCompletionsLimits{})

	body := `{"model":"mock-model","messages":[{"role":"user","content":"hi"}],"stream":true}`
	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body))
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 with no TTFT recorder configured", rec.Code)
	}
}
