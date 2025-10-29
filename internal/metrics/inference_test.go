package metrics_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/metrics"
)

func TestInferenceCollectors_ObserveTimeToFirstToken_ExposedWithLabels(t *testing.T) {
	reg := metrics.NewRegistry()
	collectors := metrics.NewInferenceCollectors(reg)

	collectors.ObserveTimeToFirstToken("llama-2-7b", "fp8", 150*time.Millisecond)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, req)

	body := rec.Body.String()
	if !strings.Contains(body, `redline_time_to_first_token_seconds`) {
		t.Fatalf("body missing the ttft metric family; body:\n%s", body)
	}
	if !strings.Contains(body, `model="llama-2-7b"`) || !strings.Contains(body, `quantization="fp8"`) {
		t.Errorf("body missing expected labels; body:\n%s", body)
	}
}
