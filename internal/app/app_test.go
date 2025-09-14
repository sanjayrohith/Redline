package app

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/config"
)

func TestAppConstructsServesAndShutsDownCleanly(t *testing.T) {
	cfg := &config.Config{
		ListenAddr:  "127.0.0.1:0",
		Environment: "test",
		LogLevel:    "error",
	}

	a := New(cfg)

	listener, err := net.Listen("tcp", cfg.ListenAddr)
	if err != nil {
		t.Fatalf("reserve address: %v", err)
	}
	addr := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatalf("release reserved address: %v", err)
	}
	a.server.Addr = addr

	ctx, cancel := context.WithCancel(context.Background())

	runErr := make(chan error, 1)
	go func() { runErr <- a.Run(ctx) }()

	waitUntilListening(t, addr)

	cancel()

	select {
	case err := <-runErr:
		if err != nil {
			t.Fatalf("Run() error = %v, want nil after graceful shutdown", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run() did not return within 5s of context cancellation")
	}
}

func waitUntilListening(t *testing.T, addr string) {
	t.Helper()

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		conn, err := net.DialTimeout("tcp", addr, 100*time.Millisecond)
		if err == nil {
			_ = conn.Close()
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("server never started listening on %s", addr)
}
