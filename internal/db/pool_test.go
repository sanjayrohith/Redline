package db

import (
	"context"
	"testing"
	"time"
)

func TestNewPool_InvalidDSN(t *testing.T) {
	_, err := NewPool(context.Background(), "not-a-valid-dsn", 5, time.Second)
	if err == nil {
		t.Fatal("NewPool() error = nil, want error for invalid dsn")
	}
}

func TestPool_HealthCheckFailsWhenUnreachable(t *testing.T) {
	ctx := context.Background()

	pool, err := NewPool(ctx, "postgres://user:pass@10.255.255.1:5432/db", 1, 500*time.Millisecond)
	if err != nil {
		t.Fatalf("NewPool() error = %v, want nil (connections are lazy)", err)
	}
	defer pool.Close()

	healthCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()

	if err := pool.HealthCheck(healthCtx); err == nil {
		t.Fatal("HealthCheck() error = nil, want error for unreachable host")
	}
}
