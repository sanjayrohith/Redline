package httpmw

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/sanjayrohith/redline/internal/apierror"
	"github.com/sanjayrohith/redline/internal/ratelimit"
)

// RateLimiter is the sliding-window decision maker RateLimit depends on.
// It is satisfied by *ratelimit.Limiter; the interface exists so this
// middleware is testable with a fake decision source.
type RateLimiter interface {
	Allow(ctx context.Context, key string, limit int, window time.Duration) (ratelimit.Decision, error)
}

// RateLimit rejects requests past limit within window, keyed by keyFunc.
// On a Redis error it fails open (allows the request) rather than taking
// the gateway down over a rate-limiting outage.
func RateLimit(limiter RateLimiter, limit int, window time.Duration, keyFunc func(*http.Request) string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			decision, err := limiter.Allow(r.Context(), keyFunc(r), limit, window)
			if err != nil {
				next.ServeHTTP(w, r)
				return
			}

			if !decision.Allowed {
				w.Header().Set("Retry-After", strconv.Itoa(max(1, int(decision.RetryAfter.Seconds()+0.5))))
				requestID, _ := RequestIDFromContext(r.Context())
				apierror.Write(w, http.StatusTooManyRequests, apierror.CodeRateLimited, "rate limit exceeded", requestID)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// PrincipalRouteKey builds a rate-limit key from the authenticated
// principal (attached by APIKeyAuth or JWTAuth) and a caller-supplied
// route class, so limits are enforced per principal and per route class.
func PrincipalRouteKey(routeClass string) func(*http.Request) string {
	return func(r *http.Request) string {
		principal, ok := PrincipalFromContext(r.Context())
		if !ok {
			return fmt.Sprintf("ratelimit:%s:anonymous", routeClass)
		}

		id := principal.UserID
		if principal.APIKeyID != "" {
			id = principal.APIKeyID
		}
		return fmt.Sprintf("ratelimit:%s:%s", routeClass, id)
	}
}
