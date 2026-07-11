package policy_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/policy"
)

func withPrincipal(req *http.Request, userID string) *http.Request {
	return req.WithContext(httpmw.WithPrincipal(req.Context(), &httpmw.Principal{UserID: userID}))
}

func TestAbuseGuardMiddleware_ReducedAccountIsRateLimitedIndependently(t *testing.T) {
	client := newTestRedisClient(t)
	reduced := policy.NewRateReductionStore(client)
	if err := reduced.Reduce(context.Background(), "user-1", time.Hour); err != nil {
		t.Fatalf("Reduce() error = %v", err)
	}

	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.AbuseThresholds{
		CadenceLimit: 1000, CadenceWindow: time.Minute,
		LowEntropyBurstLimit: 1000, LowEntropyWindow: time.Minute, EntropyThreshold: policy.LowEntropyThreshold,
		AllocationChurnLimit: 1000, AllocationChurnWindow: time.Minute,
	})
	escalator := policy.NewEscalator(newFakeViolationCounter(), &fakeRateReducer{}, &fakeSuspender{}, testThresholds())

	var reachedHandler int
	handler := policy.AbuseGuardMiddleware(detector, escalator, reduced, 1, time.Minute, limiter)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reachedHandler++ }))

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`)), "user-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || reachedHandler != 1 {
		t.Fatalf("first request: status = %d, reachedHandler = %d, want 200 and 1 (within the reduced budget)", rec.Code, reachedHandler)
	}

	req2 := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{}`)), "user-1")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusTooManyRequests {
		t.Errorf("second request status = %d, want 429 (reduced budget of 1 exceeded)", rec2.Code)
	}
	if reachedHandler != 1 {
		t.Errorf("reachedHandler = %d, want 1 (second request must not reach the handler)", reachedHandler)
	}
}

func TestAbuseGuardMiddleware_UnreducedAccountPassesThrough(t *testing.T) {
	client := newTestRedisClient(t)
	reduced := policy.NewRateReductionStore(client)

	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.DefaultAbuseThresholds)
	escalator := policy.NewEscalator(newFakeViolationCounter(), &fakeRateReducer{}, &fakeSuspender{}, testThresholds())

	var reachedHandler bool
	handler := policy.AbuseGuardMiddleware(detector, escalator, reduced, 1, time.Minute, limiter)(
		http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reachedHandler = true }))

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/chat/completions",
		strings.NewReader(`{"messages":[{"content":"hello there, please help me plan a trip"}]}`)), "user-2")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK || !reachedHandler {
		t.Errorf("status = %d reachedHandler = %v, want 200 and true", rec.Code, reachedHandler)
	}
}

func TestAbuseGuardMiddleware_PreservesRequestBodyForTheHandler(t *testing.T) {
	client := newTestRedisClient(t)
	reduced := policy.NewRateReductionStore(client)
	limiter := newFakeSignalLimiter()
	detector := policy.NewAbuseDetector(limiter, policy.DefaultAbuseThresholds)
	escalator := policy.NewEscalator(newFakeViolationCounter(), &fakeRateReducer{}, &fakeSuspender{}, testThresholds())

	const body = `{"messages":[{"content":"a normal prompt about the weather today"}]}`
	var readBody string
	handler := policy.AbuseGuardMiddleware(detector, escalator, reduced, 100, time.Minute, limiter)(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			buf := make([]byte, len(body))
			n, _ := r.Body.Read(buf)
			readBody = string(buf[:n])
		}))

	req := withPrincipal(httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)), "user-3")
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readBody != body {
		t.Errorf("readBody = %q, want the original body preserved for the handler", readBody)
	}
}
