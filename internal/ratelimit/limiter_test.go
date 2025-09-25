package ratelimit_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/sanjayrohith/redline/internal/ratelimit"
	"github.com/sanjayrohith/redline/internal/redisclient"
)

func newTestClient(t *testing.T) *redisclient.Client {
	t.Helper()

	ctx := context.Background()
	container, err := tcredis.Run(ctx, "redis:7-alpine")
	if err != nil {
		t.Skipf("skipping integration test: could not start redis container: %v", err)
	}
	t.Cleanup(func() {
		if err := container.Terminate(context.Background()); err != nil {
			t.Logf("terminate redis container: %v", err)
		}
	})

	uri, err := container.ConnectionString(ctx)
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}

	addr := uri
	const schemePrefix = "redis://"
	if len(addr) > len(schemePrefix) && addr[:len(schemePrefix)] == schemePrefix {
		addr = addr[len(schemePrefix):]
	}

	client := redisclient.NewClient(redisclient.Options{
		Addr:        addr,
		PoolSize:    10,
		MaxRetries:  3,
		DialTimeout: 5 * time.Second,
	})
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

func TestLimiter_AllowsWithinLimitThenRejects(t *testing.T) {
	client := newTestClient(t)
	limiter := ratelimit.NewLimiter(client)
	ctx := context.Background()

	key := "test:allow-then-reject"
	const limit = 3
	window := time.Second

	for i := 0; i < limit; i++ {
		decision, err := limiter.Allow(ctx, key, limit, window)
		if err != nil {
			t.Fatalf("Allow() error = %v", err)
		}
		if !decision.Allowed {
			t.Fatalf("request %d: Allowed = false, want true", i)
		}
	}

	decision, err := limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if decision.Allowed {
		t.Fatal("Allowed = true, want false once limit is exceeded")
	}
	if decision.RetryAfter <= 0 {
		t.Errorf("RetryAfter = %v, want > 0", decision.RetryAfter)
	}
}

func TestLimiter_WindowSlidesOpenAgain(t *testing.T) {
	client := newTestClient(t)
	limiter := ratelimit.NewLimiter(client)
	ctx := context.Background()

	key := "test:window-slides"
	const limit = 1
	window := 300 * time.Millisecond

	first, err := limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !first.Allowed {
		t.Fatal("first request should be allowed")
	}

	blocked, err := limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if blocked.Allowed {
		t.Fatal("second immediate request should be blocked")
	}

	time.Sleep(window + 100*time.Millisecond)

	after, err := limiter.Allow(ctx, key, limit, window)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !after.Allowed {
		t.Fatal("request after the window elapsed should be allowed")
	}
}

func TestLimiter_IndependentKeysDoNotInterfere(t *testing.T) {
	client := newTestClient(t)
	limiter := ratelimit.NewLimiter(client)
	ctx := context.Background()

	window := time.Second

	first, err := limiter.Allow(ctx, "test:key-a", 1, window)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !first.Allowed {
		t.Fatal("first key should be allowed")
	}

	second, err := limiter.Allow(ctx, "test:key-b", 1, window)
	if err != nil {
		t.Fatalf("Allow() error = %v", err)
	}
	if !second.Allowed {
		t.Fatal("independent key should not be affected by key-a's usage")
	}
}

// TestLimiter_ConcurrentRequestsRespectExactLimit drives many goroutines at
// the same key simultaneously and asserts the Lua script's atomicity holds
// under real contention: exactly limit requests are allowed, never more,
// never fewer due to a lost update.
func TestLimiter_ConcurrentRequestsRespectExactLimit(t *testing.T) {
	client := newTestClient(t)
	limiter := ratelimit.NewLimiter(client)
	ctx := context.Background()

	const (
		limit       = 20
		concurrency = 100
	)
	window := 10 * time.Second
	key := "test:concurrent-exact-limit"

	var (
		wg      sync.WaitGroup
		allowed atomic.Int64
		denied  atomic.Int64
		errs    atomic.Int64
	)

	start := make(chan struct{})
	for i := 0; i < concurrency; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			decision, err := limiter.Allow(ctx, key, limit, window)
			if err != nil {
				errs.Add(1)
				return
			}
			if decision.Allowed {
				allowed.Add(1)
			} else {
				denied.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if got := errs.Load(); got != 0 {
		t.Fatalf("errs = %d, want 0", got)
	}
	if got := allowed.Load(); got != limit {
		t.Errorf("allowed = %d, want exactly %d", got, limit)
	}
	if got := denied.Load(); got != concurrency-limit {
		t.Errorf("denied = %d, want exactly %d", got, concurrency-limit)
	}
}
