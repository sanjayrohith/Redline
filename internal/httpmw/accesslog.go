package httpmw

import (
	"log/slog"
	"net/http"
	"time"
)

// AccessLog logs one structured line per completed request: method, path,
// status, duration, and the request id RequestID attached upstream.
func AccessLog(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			rec := &statusRecorder{ResponseWriter: w, status: http.StatusOK}

			next.ServeHTTP(rec, r)

			requestID, _ := RequestIDFromContext(r.Context())
			logger.Info("request",
				"method", r.Method,
				"path", r.URL.Path,
				"status", rec.status,
				"duration_ms", time.Since(start).Milliseconds(),
				"request_id", requestID,
			)
		})
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

// Flush implements http.Flusher by delegating to the wrapped
// ResponseWriter, if it supports flushing. Without this, embedding
// http.ResponseWriter only promotes its own three methods - Header,
// Write, WriteHeader - so a type assertion for http.Flusher against
// *statusRecorder fails even when the real underlying writer (the one
// net/http gave the handler before this middleware wrapped it) can
// flush, silently breaking every streamed response that passes through
// AccessLog, which every request does.
func (s *statusRecorder) Flush() {
	if f, ok := s.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}
