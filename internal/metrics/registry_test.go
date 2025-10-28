package metrics_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"

	"github.com/sanjayrohith/redline/internal/metrics"
)

func TestRegistry_HandlerExposesRegisteredCollectors(t *testing.T) {
	reg := metrics.NewRegistry()

	counter := prometheus.NewCounter(prometheus.CounterOpts{
		Name: "redline_test_requests_total",
		Help: "test counter",
	})
	reg.MustRegister(counter)
	counter.Add(3)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	if !strings.Contains(string(body), "redline_test_requests_total 3") {
		t.Errorf("body does not contain the registered counter's value; body:\n%s", body)
	}
}

func TestRegistry_HandlerOmitsUnregisteredDefaultCollectors(t *testing.T) {
	reg := metrics.NewRegistry()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, req)

	body, err := io.ReadAll(rec.Body)
	if err != nil {
		t.Fatalf("read body: %v", err)
	}

	// go_goroutines is one of the Go runtime collectors
	// prometheus.DefaultRegisterer registers automatically; a fresh
	// Registry with nothing explicitly registered must not expose it.
	if strings.Contains(string(body), "go_goroutines") {
		t.Errorf("body unexpectedly contains a default-registerer collector; a fresh Registry should start empty. body:\n%s", body)
	}
}
