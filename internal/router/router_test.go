package router

import (
	"bytes"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testLogger(buf *bytes.Buffer) *slog.Logger {
	return slog.New(slog.NewJSONHandler(buf, nil))
}

func TestRouter_RecoversPanicsFromHandlers(t *testing.T) {
	var buf bytes.Buffer
	rt := New(Config{Logger: testLogger(&buf), Timeout: time.Second, CORSOrigins: []string{"*"}})
	rt.Mux.HandleFunc("/panics", func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/panics", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
}

func TestRouter_AttachesRequestIDAndLogsIt(t *testing.T) {
	var buf bytes.Buffer
	rt := New(Config{Logger: testLogger(&buf), Timeout: time.Second, CORSOrigins: []string{"*"}})
	rt.Mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/ok", nil))

	requestID := rec.Header().Get("X-Request-Id")
	if requestID == "" {
		t.Fatal("X-Request-Id header was not set")
	}
	if !strings.Contains(buf.String(), requestID) {
		t.Errorf("access log %q does not contain request id %q", buf.String(), requestID)
	}
}

func TestRouter_AppliesCORSHeaders(t *testing.T) {
	var buf bytes.Buffer
	rt := New(Config{Logger: testLogger(&buf), Timeout: time.Second, CORSOrigins: []string{"https://app.example.com"}})
	rt.Mux.HandleFunc("/ok", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/ok", nil)
	req.Header.Set("Origin", "https://app.example.com")

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, req)

	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://app.example.com" {
		t.Errorf("Access-Control-Allow-Origin = %q", got)
	}
}

func TestRouter_EnforcesTimeout(t *testing.T) {
	var buf bytes.Buffer
	rt := New(Config{Logger: testLogger(&buf), Timeout: 10 * time.Millisecond, CORSOrigins: []string{"*"}})
	rt.Mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
			w.WriteHeader(http.StatusOK)
		case <-r.Context().Done():
		}
	})

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestRouter_ExemptPrefixPreservesFlusher(t *testing.T) {
	var buf bytes.Buffer
	rt := New(Config{
		Logger: testLogger(&buf), Timeout: time.Second, CORSOrigins: []string{"*"},
		TimeoutExemptPrefixes: []string{"/v1/stream"},
	})

	var sawFlusher bool
	rt.Mux.HandleFunc("/v1/stream", func(w http.ResponseWriter, _ *http.Request) {
		_, sawFlusher = w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stream", nil))

	if !sawFlusher {
		t.Error("handler under an exempt prefix did not receive a ResponseWriter implementing http.Flusher")
	}
}

func TestRouter_NonExemptPathLosesFlusherUnderTimeout(t *testing.T) {
	// Documents the underlying stdlib limitation TimeoutExemptPrefixes
	// exists to work around: without the exemption, a streaming route's
	// ResponseWriter does not implement http.Flusher.
	var buf bytes.Buffer
	rt := New(Config{Logger: testLogger(&buf), Timeout: time.Second, CORSOrigins: []string{"*"}})

	var sawFlusher bool
	rt.Mux.HandleFunc("/v1/stream", func(w http.ResponseWriter, _ *http.Request) {
		_, sawFlusher = w.(http.Flusher)
		w.WriteHeader(http.StatusOK)
	})

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stream", nil))

	if sawFlusher {
		t.Error("expected the non-exempt route to lose Flusher under the Timeout middleware's wrapping")
	}
}

func TestRouter_MiddlewareOrderSurvivesTimeout(t *testing.T) {
	// A timed-out request must still have received a request id and been
	// access-logged - proving RequestID and AccessLog wrap Timeout, not
	// the other way around.
	var buf bytes.Buffer
	rt := New(Config{Logger: testLogger(&buf), Timeout: 10 * time.Millisecond, CORSOrigins: []string{"*"}})
	rt.Mux.HandleFunc("/slow", func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	})

	rec := httptest.NewRecorder()
	rt.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/slow", nil))

	if rec.Header().Get("X-Request-Id") == "" {
		t.Error("X-Request-Id should still be set on a timed-out response")
	}
	if !strings.Contains(buf.String(), `"path":"/slow"`) {
		t.Errorf("access log should still record the timed-out request, got %q", buf.String())
	}
}
