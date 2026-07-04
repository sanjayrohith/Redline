package resilience_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/httpmw"
	"github.com/sanjayrohith/redline/internal/resilience"
)

func TestMiddleware_TracksRequestForItsDuration(t *testing.T) {
	w := resilience.NewWatchdog(time.Hour)

	var sawCount int
	handler := httpmw.RequestID(resilience.Middleware(w)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		sawCount = w.InFlightCount()
	})))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"m"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if sawCount != 1 {
		t.Errorf("InFlightCount() during the handler = %d, want 1", sawCount)
	}
	if n := w.InFlightCount(); n != 0 {
		t.Errorf("InFlightCount() after the handler returned = %d, want 0 (forgotten)", n)
	}
}

func TestMiddleware_PreservesRequestBodyForTheHandler(t *testing.T) {
	w := resilience.NewWatchdog(time.Hour)

	var readBody string
	handler := httpmw.RequestID(resilience.Middleware(w)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		readBody = string(b)
	})))

	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"model":"m"}`))
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if readBody != `{"model":"m"}` {
		t.Errorf("readBody = %q, want the original body preserved for the handler", readBody)
	}
}

func TestMiddleware_SkipsTrackingWhenNoRequestID(t *testing.T) {
	w := resilience.NewWatchdog(time.Hour)

	var sawCount int
	handler := resilience.Middleware(w)(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		sawCount = w.InFlightCount()
	}))

	req := httptest.NewRequest(http.MethodPost, "/", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	if sawCount != 0 {
		t.Errorf("InFlightCount() = %d, want 0 when the request carries no request id", sawCount)
	}
}
