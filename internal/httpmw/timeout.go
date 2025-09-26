package httpmw

import (
	"net/http"
	"time"
)

// timeoutBody matches apierror's envelope shape. It cannot carry a
// request id: http.TimeoutHandler's message is fixed at construction, long
// before any individual request exists. The id is still recoverable from
// the response's X-Request-Id header, set upstream by RequestID.
const timeoutBody = `{"error":{"code":"request_timeout","message":"request timed out"}}`

// Timeout bounds every request to d, responding 503 if the handler has not
// finished by then. It delegates to the standard library's TimeoutHandler,
// which safely serializes the handler's writes against the timeout path
// rather than risking a data race on the ResponseWriter.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, timeoutBody)
	}
}
