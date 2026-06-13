package telemetry

import "time"

// MetricsRecorder is the Prometheus-facing dependency Publisher wraps. It
// is satisfied by *metrics.InferenceCollectors.
type MetricsRecorder interface {
	ObserveTimeToFirstToken(model, quantization string, d time.Duration)
	ObserveInterTokenLatency(model, quantization string, d time.Duration)
}

// Publisher implements the api package's narrow TTFTRecorder and
// TPOTRecorder interfaces, fanning every observation out to two
// destinations: the durable Prometheus histograms metrics already
// records, and this process's live Hub, so a connected dashboard sees
// the same latency numbers a scrape would eventually show, without
// waiting on a scrape interval.
//
// Sample.RunID carries the model name, not a per-request id: neither
// TTFTRecorder nor TPOTRecorder's signature (fixed by the api package,
// used by both interfaces) carries a request identity, only model and
// quantization. A live chart grouping by model is what that constraint
// allows; per-request correlation would require widening that interface.
type Publisher struct {
	hub     *Hub
	metrics MetricsRecorder
}

// NewPublisher returns a Publisher fanning observations out to hub and metrics.
func NewPublisher(hub *Hub, metrics MetricsRecorder) *Publisher {
	return &Publisher{hub: hub, metrics: metrics}
}

// ObserveTimeToFirstToken implements api.TTFTRecorder.
func (p *Publisher) ObserveTimeToFirstToken(model, quantization string, d time.Duration) {
	p.metrics.ObserveTimeToFirstToken(model, quantization, d)
	ms := float64(d.Milliseconds())
	p.hub.Publish(Sample{RunID: model, TTFTMs: &ms, SampledAt: time.Now()})
}

// ObserveInterTokenLatency implements api.TPOTRecorder.
func (p *Publisher) ObserveInterTokenLatency(model, quantization string, d time.Duration) {
	p.metrics.ObserveInterTokenLatency(model, quantization, d)
	ms := float64(d.Milliseconds())
	p.hub.Publish(Sample{RunID: model, TPOTMs: &ms, SampledAt: time.Now()})
}
