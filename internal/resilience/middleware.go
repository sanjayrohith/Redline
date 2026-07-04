package resilience

import (
	"bytes"
	"io"
	"net/http"

	"github.com/sanjayrohith/redline/internal/httpmw"
)

// Middleware registers every request passing through it with watchdog for
// stuck-detection, and stops tracking it once the handler returns -
// whether it finished normally, errored, or (for a streamed response) the
// client disconnected, all of which unwind back through this deferred
// call the same way.
func Middleware(watchdog *Watchdog) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			requestID, _ := httpmw.RequestIDFromContext(r.Context())
			if requestID == "" {
				next.ServeHTTP(w, r)
				return
			}

			body, err := io.ReadAll(r.Body)
			if err == nil {
				r.Body = io.NopCloser(bytes.NewReader(body))
			}

			watchdog.Start(requestID, RequeuePayload{RequestID: requestID, Body: body})
			defer watchdog.Forget(requestID)

			next.ServeHTTP(w, r)
		})
	}
}
