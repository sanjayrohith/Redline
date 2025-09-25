package httpmw

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
)

type requestIDContextKey struct{}

const requestIDHeader = "X-Request-Id"

// RequestID attaches a request id - the caller's incoming header if
// present, otherwise a freshly generated one - to the request context and
// echoes it back on the response, so every later middleware and handler
// can correlate logs to one request.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(requestIDHeader)
		if id == "" {
			id = generateRequestID()
		}

		w.Header().Set(requestIDHeader, id)
		ctx := context.WithValue(r.Context(), requestIDContextKey{}, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequestIDFromContext returns the request id attached by RequestID, if any.
func RequestIDFromContext(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDContextKey{}).(string)
	return id, ok
}

func generateRequestID() string {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		// crypto/rand failing is effectively unrecoverable on this host;
		// fall back to a fixed, clearly-marked id rather than panicking
		// on a hot request path.
		return "req-unavailable"
	}
	return hex.EncodeToString(raw)
}
