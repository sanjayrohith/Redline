// Package app wires the gateway's dependency graph and owns its lifecycle.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/sanjayrohith/redline/internal/config"
	"github.com/sanjayrohith/redline/internal/logging"
)

// App holds every dependency the gateway needs to serve traffic and owns
// the HTTP server's start/stop lifecycle.
type App struct {
	server *http.Server
	logger *slog.Logger
}

// New constructs the dependency graph and returns a ready-to-run App.
func New(cfg *config.Config) *App {
	mux := http.NewServeMux()
	logger := logging.New(cfg.LogLevel)

	return &App{
		server: &http.Server{
			Addr:              cfg.ListenAddr,
			Handler:           mux,
			ReadHeaderTimeout: 5 * time.Second,
		},
		logger: logger,
	}
}

// Run starts the HTTP server and blocks until ctx is cancelled, at which
// point it gracefully shuts the server down.
func (a *App) Run(ctx context.Context) error {
	listener, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return fmt.Errorf("listen on %s: %w", a.server.Addr, err)
	}

	serveErr := make(chan error, 1)
	go func() {
		if err := a.server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			serveErr <- err
			return
		}
		serveErr <- nil
	}()

	a.logger.Info("gateway listening", "addr", a.server.Addr)

	select {
	case err := <-serveErr:
		return err
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")

		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown: %w", err)
		}
		return nil
	}
}
