package httpmw

import (
	"net/http"
	"time"
)

// Timeout bounds every request to d, responding 503 if the handler has not
// finished by then.
func Timeout(d time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.TimeoutHandler(next, d, `{"error":"request_timeout"}`)
	}
}
