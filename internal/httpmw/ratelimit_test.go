package httpmw

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/ratelimit"
)

type fakeRateLimiter struct {
	decision ratelimit.Decision
	err      error
	lastKey  string
}

func (f *fakeRateLimiter) Allow(_ context.Context, key string, _ int, _ time.Duration) (ratelimit.Decision, error) {
	f.lastKey = key
	return f.decision, f.err
}

func TestRateLimit_AllowsUnderLimit(t *testing.T) {
	limiter := &fakeRateLimiter{decision: ratelimit.Decision{Allowed: true}}
	handler := RateLimit(limiter, 10, time.Second, PrincipalRouteKey("chat"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestRateLimit_RejectsOverLimitWithRetryAfter(t *testing.T) {
	limiter := &fakeRateLimiter{decision: ratelimit.Decision{Allowed: false, RetryAfter: 3 * time.Second}}
	handler := RateLimit(limiter, 10, time.Second, PrincipalRouteKey("chat"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Errorf("Retry-After = %q, want 3", got)
	}
	if got := rec.Body.String(); got != `{"error":"rate_limited"}`+"\n" {
		t.Errorf("body = %q", got)
	}
}

func TestRateLimit_FailsOpenOnLimiterError(t *testing.T) {
	limiter := &fakeRateLimiter{err: errBoom}
	handler := RateLimit(limiter, 10, time.Second, PrincipalRouteKey("chat"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (fail open)", rec.Code)
	}
}

func TestPrincipalRouteKey_DistinguishesPrincipalsAndRoutes(t *testing.T) {
	limiter := &fakeRateLimiter{decision: ratelimit.Decision{Allowed: true}}
	handler := RateLimit(limiter, 10, time.Second, PrincipalRouteKey("chat"))(
		http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }),
	)

	req := httptest.NewRequest(http.MethodPost, "/", nil).
		WithContext(WithPrincipal(context.Background(), &Principal{APIKeyID: "key-1"}))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if limiter.lastKey != "ratelimit:chat:key-1" {
		t.Errorf("key = %q, want ratelimit:chat:key-1", limiter.lastKey)
	}

	anon := httptest.NewRequest(http.MethodPost, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), anon)

	if limiter.lastKey != "ratelimit:chat:anonymous" {
		t.Errorf("key = %q, want ratelimit:chat:anonymous", limiter.lastKey)
	}
}

var errBoom = errTestBoom("boom")

type errTestBoom string

func (e errTestBoom) Error() string { return string(e) }
