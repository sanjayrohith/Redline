package scheduler_test

import (
	"context"
	"errors"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/sanjayrohith/redline/internal/redisclient"
	"github.com/sanjayrohith/redline/internal/scheduler"
)

func newTestRedisClient(t *testing.T) *redisclient.Client {
	t.Helper()
	ctx := context.Background()

	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping integration test: could not start redis container: %v", err)
	}
	t.Cleanup(func() { _ = container.Terminate(context.Background()) })

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	addr := uri
	const prefix = "redis://"
	if len(addr) > len(prefix) && addr[:len(prefix)] == prefix {
		addr = addr[len(prefix):]
	}

	client := redisclient.NewClient(redisclient.Options{Addr: addr, PoolSize: 10, MaxRetries: 3, DialTimeout: 5 * time.Second})
	t.Cleanup(func() { _ = client.Close() })

	deadline := time.Now().Add(15 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		checkCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
		lastErr = client.HealthCheck(checkCtx)
		cancel()
		if lastErr == nil {
			return client
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("redis never became healthy: %v", lastErr)
	return nil
}

func TestIdleTracker_TouchAndIdleDuration(t *testing.T) {
	client := newTestRedisClient(t)
	tracker := scheduler.NewIdleTracker(client)
	ctx := context.Background()

	if err := tracker.Touch(ctx, "alloc-1"); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	idle, err := tracker.IdleDuration(ctx, "alloc-1")
	if err != nil {
		t.Fatalf("IdleDuration() error = %v", err)
	}
	if idle < 100*time.Millisecond {
		t.Errorf("IdleDuration() = %v, want at least ~150ms", idle)
	}
}

func TestIdleTracker_TouchResetsIdleDuration(t *testing.T) {
	client := newTestRedisClient(t)
	tracker := scheduler.NewIdleTracker(client)
	ctx := context.Background()

	if err := tracker.Touch(ctx, "alloc-1"); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	if err := tracker.Touch(ctx, "alloc-1"); err != nil {
		t.Fatalf("second Touch() error = %v", err)
	}

	idle, err := tracker.IdleDuration(ctx, "alloc-1")
	if err != nil {
		t.Fatalf("IdleDuration() error = %v", err)
	}
	if idle > 100*time.Millisecond {
		t.Errorf("IdleDuration() = %v, want close to 0 right after a fresh Touch", idle)
	}
}

func TestIdleTracker_NoRecord(t *testing.T) {
	client := newTestRedisClient(t)
	tracker := scheduler.NewIdleTracker(client)

	if _, err := tracker.IdleDuration(context.Background(), "never-touched"); !errors.Is(err, scheduler.ErrNoIdleRecord) {
		t.Errorf("error = %v, want ErrNoIdleRecord", err)
	}
}

func TestIdleTracker_Forget(t *testing.T) {
	client := newTestRedisClient(t)
	tracker := scheduler.NewIdleTracker(client)
	ctx := context.Background()

	if err := tracker.Touch(ctx, "alloc-1"); err != nil {
		t.Fatalf("Touch() error = %v", err)
	}
	if err := tracker.Forget(ctx, "alloc-1"); err != nil {
		t.Fatalf("Forget() error = %v", err)
	}

	if _, err := tracker.IdleDuration(ctx, "alloc-1"); !errors.Is(err, scheduler.ErrNoIdleRecord) {
		t.Errorf("error = %v, want ErrNoIdleRecord after Forget", err)
	}
}

func TestIdleTracker_IndependentAllocations(t *testing.T) {
	client := newTestRedisClient(t)
	tracker := scheduler.NewIdleTracker(client)
	ctx := context.Background()

	if err := tracker.Touch(ctx, "alloc-a"); err != nil {
		t.Fatalf("Touch(a) error = %v", err)
	}
	if err := tracker.Forget(ctx, "alloc-b"); err != nil {
		t.Fatalf("Forget(b) error = %v", err)
	}

	if _, err := tracker.IdleDuration(ctx, "alloc-a"); err != nil {
		t.Errorf("IdleDuration(a) error = %v, want nil (a was touched, unaffected by b)", err)
	}
}
