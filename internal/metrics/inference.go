package metrics

import (
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

// InferenceCollectors are the metric collectors instrumenting the
// gateway's actual inference request path.
type InferenceCollectors struct {
	timeToFirstToken  *prometheus.HistogramVec
	interTokenLatency *prometheus.HistogramVec
}

// NewInferenceCollectors builds the inference-path metric collectors and
// registers them against reg.
func NewInferenceCollectors(reg *Registry) *InferenceCollectors {
	ttft := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "redline_time_to_first_token_seconds",
		Help: "Time from request admission to the first generated token being flushed to the client, measured at the gateway boundary - " +
			"where the client actually observes latency, not internal engine timings that never leave the node.",
		Buckets: prometheus.DefBuckets,
	}, []string{"model", "quantization"})
	reg.MustRegister(ttft)

	tpot := prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name: "redline_inter_token_latency_seconds",
		Help: "Time between consecutive tokens during the decode phase (time per output token) - the metric that actually " +
			"determines a user's perceived generation speed once the first token has already arrived.",
		Buckets: prometheus.DefBuckets,
	}, []string{"model", "quantization"})
	reg.MustRegister(tpot)

	return &InferenceCollectors{timeToFirstToken: ttft, interTokenLatency: tpot}
}

// ObserveTimeToFirstToken records one request's admission-to-first-token
// duration, labeled by the model served and the precision it was
// quantized to.
func (c *InferenceCollectors) ObserveTimeToFirstToken(model, quantization string, d time.Duration) {
	c.timeToFirstToken.WithLabelValues(model, quantization).Observe(d.Seconds())
}

// ObserveInterTokenLatency records the gap between two consecutive
// tokens of the same response's decode phase, labeled by model and
// quantization. It is never called for the gap before the first token -
// that is time to first token, a distinct measurement covering prefill
// and queueing, not decode-phase behavior.
func (c *InferenceCollectors) ObserveInterTokenLatency(model, quantization string, d time.Duration) {
	c.interTokenLatency.WithLabelValues(model, quantization).Observe(d.Seconds())
}
