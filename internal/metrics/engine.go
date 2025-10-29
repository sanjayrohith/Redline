package metrics

import "github.com/prometheus/client_golang/prometheus"

// EngineCollectors exposes an inference engine's own internal state -
// scraped and relabeled from its own /metrics endpoint via
// ScrapeVLLMEngineMetrics - as gauges in the platform's namespace,
// labeled by the model the scraped engine is serving.
type EngineCollectors struct {
	requestsRunning *prometheus.GaugeVec
	requestsWaiting *prometheus.GaugeVec
	kvCacheUsage    *prometheus.GaugeVec
}

// NewEngineCollectors builds the engine-scrape gauges and registers them
// against reg.
func NewEngineCollectors(reg *Registry) *EngineCollectors {
	requestsRunning := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_engine_requests_running",
		Help: "Requests currently being decoded by the engine, as the engine itself reports it.",
	}, []string{"model"})
	requestsWaiting := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_engine_requests_waiting",
		Help: "Requests admitted but not yet scheduled onto the engine, as the engine itself reports it.",
	}, []string{"model"})
	kvCacheUsage := prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "redline_engine_kv_cache_usage_ratio",
		Help: "Fraction of the engine's KV cache currently in use, as the engine itself reports it.",
	}, []string{"model"})

	reg.MustRegister(requestsRunning, requestsWaiting, kvCacheUsage)

	return &EngineCollectors{
		requestsRunning: requestsRunning,
		requestsWaiting: requestsWaiting,
		kvCacheUsage:    kvCacheUsage,
	}
}

// Update sets each gauge from samples - the result of one
// ScrapeVLLMEngineMetrics call - labeled by model. A relabeled metric
// name samples does not contain is left at its previous value; a sample
// this deployment never expects can arrive if the engine's own metric
// surface grows in a future version, and is silently ignored rather than
// treated as an error, since that only means the platform namespace has
// not caught up yet, not that the scrape itself failed.
func (c *EngineCollectors) Update(model string, samples []EngineSample) {
	for _, s := range samples {
		switch s.Name {
		case "redline_engine_requests_running":
			c.requestsRunning.WithLabelValues(model).Set(s.Value)
		case "redline_engine_requests_waiting":
			c.requestsWaiting.WithLabelValues(model).Set(s.Value)
		case "redline_engine_kv_cache_usage_ratio":
			c.kvCacheUsage.WithLabelValues(model).Set(s.Value)
		}
	}
}
