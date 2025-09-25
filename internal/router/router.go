// Package router composes the gateway's HTTP mux with its fixed
// middleware chain.
package router

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/httpmw"
)

// Config configures the router's middleware chain.
type Config struct {
	Logger      *slog.Logger
	Timeout     time.Duration
	CORSOrigins []string
}

// Router holds the raw mux, so handlers can still be registered on it
// after construction, alongside the fully wrapped Handler the HTTP server
// should actually serve.
type Router struct {
	Mux     *http.ServeMux
	Handler http.Handler
}

// New builds a Router whose middleware runs, for every request, in this
// fixed order: recovery, request id, access logging, timeout, then CORS,
// before reaching a registered route.
func New(cfg Config) *Router {
	mux := http.NewServeMux()

	var handler http.Handler = mux
	handler = httpmw.CORS(cfg.CORSOrigins)(handler)
	handler = httpmw.Timeout(cfg.Timeout)(handler)
	handler = httpmw.AccessLog(cfg.Logger)(handler)
	handler = httpmw.RequestID(handler)
	handler = httpmw.Recovery(cfg.Logger)(handler)

	return &Router{Mux: mux, Handler: handler}
}
