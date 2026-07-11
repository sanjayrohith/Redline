package httpmw

import (
	"net/http"
	"strings"
	"time"
)

// timeoutBody matches apierror's envelope shape. It cannot carry a
// request id: http.TimeoutHandler's message is fixed at construction, long
// before any individual request exists. The id is still recoverable from
// the response's X-Request-Id header, set upstream by RequestID.
const timeoutBody = `{"error":{"code":"request_timeout","message":"request timed out"}}`

// Timeout bounds every request whose path does not start with one of
// exemptPrefixes to d, responding 503 if the handler has not finished by
// then. It delegates to the standard library's TimeoutHandler, which
// safely serializes the handler's writes against the timeout path rather
// than risking a data race on the ResponseWriter - but that safety comes
// from wrapping the ResponseWriter in a type that does not implement
// http.Flusher, which silently breaks any route that streams a response
// (Server-Sent Events, chunked transfer). Routes under an exempt prefix
// skip the wrapper entirely and rely on their own internal deadline
// instead (e.g. ChatCompletionsLimits.GenerationTimeout), which already
// bounds generation via the request context without touching the writer.
func Timeout(d time.Duration, exemptPrefixes ...string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		timeoutHandler := http.TimeoutHandler(next, d, timeoutBody)
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, prefix := range exemptPrefixes {
				if strings.HasPrefix(r.URL.Path, prefix) {
					next.ServeHTTP(w, r)
					return
				}
			}
			timeoutHandler.ServeHTTP(w, r)
		})
	}
}
