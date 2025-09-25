package redisclient

import (
	"context"
	"testing"
	"time"
)

func TestHealthCheckFailsWhenUnreachable(t *testing.T) {
	client := NewClient(Options{
		Addr:        "10.255.255.1:6379",
		PoolSize:    5,
		MaxRetries:  1,
		DialTimeout: 500 * time.Millisecond,
	})
	defer func() { _ = client.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	if err := client.HealthCheck(ctx); err == nil {
		t.Fatal("HealthCheck() error = nil, want error for unreachable host")
	}
}
