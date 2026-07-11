package policy_test

import (
	"context"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/sanjayrohith/redline/internal/policy"
	"github.com/sanjayrohith/redline/internal/redisclient"
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
	for time.Now().Before(deadline) {
		if client.HealthCheck(ctx) == nil {
			return client
		}
		time.Sleep(300 * time.Millisecond)
	}
	t.Fatal("redis never became healthy")
	return nil
}

func TestRedisViolationCounter_IncrementsAcrossCalls(t *testing.T) {
	client := newTestRedisClient(t)
	counter := policy.NewRedisViolationCounter(client)
	ctx := context.Background()

	first, err := counter.Incr(ctx, "test:violations:user-1")
	if err != nil {
		t.Fatalf("Incr() error = %v", err)
	}
	if first != 1 {
		t.Errorf("first Incr() = %d, want 1", first)
	}

	second, err := counter.Incr(ctx, "test:violations:user-1")
	if err != nil {
		t.Fatalf("Incr() error = %v", err)
	}
	if second != 2 {
		t.Errorf("second Incr() = %d, want 2", second)
	}

	if err := counter.Expire(ctx, "test:violations:user-1", time.Minute); err != nil {
		t.Fatalf("Expire() error = %v", err)
	}
}

func TestRateReductionStore_ReduceThenIsReduced(t *testing.T) {
	client := newTestRedisClient(t)
	store := policy.NewRateReductionStore(client)
	ctx := context.Background()

	before, err := store.IsReduced(ctx, "user-1")
	if err != nil {
		t.Fatalf("IsReduced() error = %v", err)
	}
	if before {
		t.Fatal("IsReduced() = true before Reduce() was ever called")
	}

	if err := store.Reduce(ctx, "user-1", time.Minute); err != nil {
		t.Fatalf("Reduce() error = %v", err)
	}

	after, err := store.IsReduced(ctx, "user-1")
	if err != nil {
		t.Fatalf("IsReduced() error = %v", err)
	}
	if !after {
		t.Error("IsReduced() = false after Reduce()")
	}

	otherUser, err := store.IsReduced(ctx, "user-2")
	if err != nil {
		t.Fatalf("IsReduced() error = %v", err)
	}
	if otherUser {
		t.Error("a reduction for user-1 must not affect user-2")
	}
}

func TestRateReductionStore_ExpiresAfterTTL(t *testing.T) {
	client := newTestRedisClient(t)
	store := policy.NewRateReductionStore(client)
	ctx := context.Background()

	if err := store.Reduce(ctx, "user-1", 50*time.Millisecond); err != nil {
		t.Fatalf("Reduce() error = %v", err)
	}
	time.Sleep(200 * time.Millisecond)

	reduced, err := store.IsReduced(ctx, "user-1")
	if err != nil {
		t.Fatalf("IsReduced() error = %v", err)
	}
	if reduced {
		t.Error("IsReduced() = true after the reduction's TTL elapsed")
	}
}
