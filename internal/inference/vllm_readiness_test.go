package inference_test

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/inference"
)

func TestReadinessProbe_CheckFailsWhileHealthEndpointIsDown(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		t.Fatalf("unexpected request to %s while unhealthy", r.URL.Path)
	}))
	defer server.Close()

	probe := inference.NewReadinessProbe(server.URL, "llama", server.Client())
	err := probe.Check(context.Background())
	if !errors.Is(err, inference.ErrEngineNotReady) {
		t.Fatalf("Check() error = %v, want ErrEngineNotReady", err)
	}
}

func TestReadinessProbe_CheckFailsWhenHealthyButGenerationFails(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/chat/completions":
			// Weights still loading: the listener is up, but generation
			// itself fails - exactly the gap a bare /health check misses.
			w.WriteHeader(http.StatusServiceUnavailable)
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer server.Close()

	probe := inference.NewReadinessProbe(server.URL, "llama", server.Client())
	err := probe.Check(context.Background())
	if !errors.Is(err, inference.ErrEngineNotReady) {
		t.Fatalf("Check() error = %v, want ErrEngineNotReady", err)
	}
}

func TestReadinessProbe_CheckSucceedsOnceHealthAndGenerationBothWork(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			w.WriteHeader(http.StatusOK)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer server.Close()

	probe := inference.NewReadinessProbe(server.URL, "llama", server.Client())
	if err := probe.Check(context.Background()); err != nil {
		t.Fatalf("Check() error = %v, want nil", err)
	}
}

func TestReadinessProbe_WaitReadyPollsUntilTheEngineWarmsUp(t *testing.T) {
	var attempts int32
	const readyAfter = 3

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/health":
			n := atomic.AddInt32(&attempts, 1)
			if n < readyAfter {
				w.WriteHeader(http.StatusServiceUnavailable)
				return
			}
			w.WriteHeader(http.StatusOK)
		case "/v1/chat/completions":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"index":0,"message":{"role":"assistant","content":"pong"},"finish_reason":"stop"}]}`))
		default:
			t.Fatalf("unexpected request to %s", r.URL.Path)
		}
	}))
	defer server.Close()

	probe := inference.NewReadinessProbe(server.URL, "llama", server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := probe.WaitReady(ctx, 20*time.Millisecond); err != nil {
		t.Fatalf("WaitReady() error = %v", err)
	}
	if got := atomic.LoadInt32(&attempts); got < readyAfter {
		t.Errorf("health endpoint was polled %d times, want at least %d", got, readyAfter)
	}
}

func TestReadinessProbe_WaitReadyRespectsContextDeadline(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/health" {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	defer server.Close()

	probe := inference.NewReadinessProbe(server.URL, "llama", server.Client())
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	err := probe.WaitReady(ctx, 20*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, inference.ErrEngineNotReady) {
		t.Fatalf("WaitReady() error = %v, want ErrEngineNotReady", err)
	}
	if elapsed > 2*time.Second {
		t.Errorf("WaitReady() took %s, want it to return promptly after the context deadline", elapsed)
	}
}
