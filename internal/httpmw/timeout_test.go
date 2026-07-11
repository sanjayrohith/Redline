package httpmw

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestTimeout_ExemptPathBypassesTheTimeoutHandler(t *testing.T) {
	var sawFlusher bool
	handler := Timeout(time.Second, "/v1/stream")(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, sawFlusher = w.(http.Flusher)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/stream/completions", nil))

	if !sawFlusher {
		t.Error("exempt path lost http.Flusher")
	}
}

func TestTimeout_NonExemptPathStillEnforcesDeadline(t *testing.T) {
	handler := Timeout(10*time.Millisecond, "/v1/stream")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/other", nil))

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", rec.Code)
	}
}

func TestTimeout_NoExemptPrefixesBehavesAsBefore(t *testing.T) {
	handler := Timeout(time.Second)(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/anything", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}
