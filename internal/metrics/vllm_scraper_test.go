package metrics_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sanjayrohith/redline/internal/metrics"
)

const sampleVLLMExposition = `# HELP vllm:num_requests_running Number of requests currently running on GPU.
# TYPE vllm:num_requests_running gauge
vllm:num_requests_running{model_name="llama-2-7b"} 4.0
# HELP vllm:num_requests_waiting Number of requests waiting to be processed.
# TYPE vllm:num_requests_waiting gauge
vllm:num_requests_waiting{model_name="llama-2-7b"} 2.0
# HELP vllm:gpu_cache_usage_perc GPU KV-cache usage. 1 means 100 percent usage.
# TYPE vllm:gpu_cache_usage_perc gauge
vllm:gpu_cache_usage_perc{model_name="llama-2-7b"} 0.73
# HELP vllm:unrelated_metric_we_dont_track Some other metric vLLM exposes.
# TYPE vllm:unrelated_metric_we_dont_track counter
vllm:unrelated_metric_we_dont_track 99.0
`

func TestScrapeVLLMEngineMetrics_ParsesAndRelabelsKnownMetrics(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/metrics" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4")
		_, _ = w.Write([]byte(sampleVLLMExposition))
	}))
	defer server.Close()

	samples, err := metrics.ScrapeVLLMEngineMetrics(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("ScrapeVLLMEngineMetrics() error = %v", err)
	}

	got := map[string]float64{}
	for _, s := range samples {
		got[s.Name] = s.Value
	}

	want := map[string]float64{
		"redline_engine_requests_running":     4.0,
		"redline_engine_requests_waiting":     2.0,
		"redline_engine_kv_cache_usage_ratio": 0.73,
	}
	for name, wantVal := range want {
		gotVal, ok := got[name]
		if !ok {
			t.Errorf("missing relabeled sample %q; got %v", name, got)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("sample %q = %v, want %v", name, gotVal, wantVal)
		}
	}

	if _, ok := got["vllm:unrelated_metric_we_dont_track"]; ok {
		t.Error("scrape included a metric outside the tracked set")
	}
	if len(samples) != 3 {
		t.Errorf("got %d samples, want exactly 3 (only the tracked metrics)", len(samples))
	}
}

func TestScrapeVLLMEngineMetrics_NonOKStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	defer server.Close()

	_, err := metrics.ScrapeVLLMEngineMetrics(context.Background(), server.Client(), server.URL)
	if err == nil {
		t.Fatal("ScrapeVLLMEngineMetrics() error = nil, want an error for a 503 response")
	}
}

func TestScrapeVLLMEngineMetrics_MissingMetricsAreOmittedNotErrored(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("# HELP some_other_metric Something else entirely.\n# TYPE some_other_metric gauge\nsome_other_metric 1.0\n"))
	}))
	defer server.Close()

	samples, err := metrics.ScrapeVLLMEngineMetrics(context.Background(), server.Client(), server.URL)
	if err != nil {
		t.Fatalf("ScrapeVLLMEngineMetrics() error = %v, want nil when tracked metrics are simply absent", err)
	}
	if len(samples) != 0 {
		t.Errorf("got %d samples, want 0", len(samples))
	}
}

func TestEngineCollectors_UpdateSetsGaugesFromSamples(t *testing.T) {
	reg := metrics.NewRegistry()
	collectors := metrics.NewEngineCollectors(reg)

	collectors.Update("llama-2-7b", []metrics.EngineSample{
		{Name: "redline_engine_requests_running", Value: 4},
		{Name: "redline_engine_requests_waiting", Value: 2},
		{Name: "redline_engine_kv_cache_usage_ratio", Value: 0.73},
	})

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	reg.Handler().ServeHTTP(rec, req)
	body := rec.Body.String()

	for _, want := range []string{
		`redline_engine_requests_running{model="llama-2-7b"} 4`,
		`redline_engine_requests_waiting{model="llama-2-7b"} 2`,
		`redline_engine_kv_cache_usage_ratio{model="llama-2-7b"} 0.73`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body missing %q; body:\n%s", want, body)
		}
	}
}
