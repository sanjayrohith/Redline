package metrics

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/common/expfmt"
)

// vllmMetricNames maps a vLLM engine metric name (as vLLM itself exposes
// it on its own /metrics endpoint) to the platform-namespaced name
// Redline relabels it to. vLLM's own metric names carry vLLM's own
// naming convention (the "vllm:" prefix); relabeling into "redline_"
// names is what lets a dashboard or alert query the platform's metrics
// uniformly regardless of which engine (vLLM, SGLang, a future backend)
// actually served a given deployment.
var vllmMetricNames = map[string]string{
	"vllm:num_requests_running": "redline_engine_requests_running",
	"vllm:num_requests_waiting": "redline_engine_requests_waiting",
	"vllm:gpu_cache_usage_perc": "redline_engine_kv_cache_usage_ratio",
}

// EngineSample is one scraped and relabeled engine metric value.
type EngineSample struct {
	Name  string
	Value float64
}

// ScrapeVLLMEngineMetrics fetches baseURL's own /metrics endpoint - vLLM
// exposes queue depth, running/waiting request counts, and KV cache
// utilization there in Prometheus text exposition format - parses it,
// and returns every metric this platform tracks, relabeled per
// vllmMetricNames. A metric family vLLM does not expose (an older engine
// version, or one this deployment's mock backend, which exposes none of
// these) is simply absent from the result, not an error.
func ScrapeVLLMEngineMetrics(ctx context.Context, httpClient *http.Client, baseURL string) ([]EngineSample, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/metrics", nil)
	if err != nil {
		return nil, fmt.Errorf("metrics: build vllm metrics scrape request: %w", err)
	}

	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("metrics: scrape vllm metrics: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("metrics: vllm metrics endpoint returned status %d", resp.StatusCode)
	}

	var parser expfmt.TextParser
	families, err := parser.TextToMetricFamilies(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("metrics: parse vllm metrics exposition: %w", err)
	}

	var samples []EngineSample
	for vllmName, relabeled := range vllmMetricNames {
		mf, ok := families[vllmName]
		if !ok {
			continue
		}
		for _, m := range mf.Metric {
			var value float64
			switch {
			case m.Gauge != nil:
				value = m.Gauge.GetValue()
			case m.Counter != nil:
				value = m.Counter.GetValue()
			default:
				continue
			}
			samples = append(samples, EngineSample{Name: relabeled, Value: value})
		}
	}

	return samples, nil
}
