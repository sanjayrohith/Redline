package httpmw

import (
	"log/slog"
	"net/http"

	"github.com/sanjayrohith/redline/internal/apierror"
)

// Recovery catches a panic anywhere downstream, logs it, and responds with
// a generic 500 instead of letting the connection die uncleanly. It must
// be the outermost middleware in the chain so it can catch panics raised
// by any later middleware too.
func Recovery(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if rec := recover(); rec != nil {
					requestID, _ := RequestIDFromContext(r.Context())
					logger.Error("panic recovered", "panic", rec, "path", r.URL.Path, "request_id", requestID)
					apierror.Write(w, http.StatusInternalServerError, apierror.CodeInternal, "internal server error", requestID)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
