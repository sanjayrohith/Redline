// Package metrics owns the gateway's Prometheus metric collectors and its
// exposition endpoint.
package metrics

import (
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Registry holds every metric collector the gateway registers, so
// individual packages (inference, scheduler, ...) receive typed
// collectors to record against rather than reaching into a global
// default registry.
type Registry struct {
	reg *prometheus.Registry
}

// NewRegistry returns a Registry backed by a fresh, empty
// prometheus.Registry - not prometheus.DefaultRegisterer - so the
// gateway's exposition surface is exactly the collectors it registers
// here, with no ambient Go runtime metrics or collectors some imported
// package registered globally as a side effect.
func NewRegistry() *Registry {
	return &Registry{reg: prometheus.NewRegistry()}
}

// MustRegister registers collectors against the registry, panicking on a
// duplicate or otherwise invalid registration - a startup-time
// programming error, not a runtime condition to recover from.
func (r *Registry) MustRegister(collectors ...prometheus.Collector) {
	r.reg.MustRegister(collectors...)
}

// Handler returns the Prometheus exposition HTTP handler for this
// registry's collectors, to be mounted at /metrics on the internal-only
// metrics listener - never on the public API listener (see
// config.Config.MetricsListenAddr's doc comment).
func (r *Registry) Handler() http.Handler {
	return promhttp.HandlerFor(r.reg, promhttp.HandlerOpts{Registry: r.reg})
}
