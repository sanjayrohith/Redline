package queue_test

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/sanjayrohith/redline/internal/queue"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestWorkerPool_RespectsConcurrencyBound(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-pool-concurrency", queue.Options{VisibilityTimeout: 10 * time.Second, PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	const jobCount = 20
	const concurrency = 3

	for i := 0; i < jobCount; i++ {
		if err := q.Enqueue(ctx, fmt.Sprintf("job-%d", i), "payload"); err != nil {
			t.Fatalf("Enqueue() error = %v", err)
		}
	}

	var (
		inFlight    atomic.Int32
		maxInFlight atomic.Int32
		processed   atomic.Int32
	)

	handler := func(_ context.Context, _ *queue.Job) error {
		n := inFlight.Add(1)
		for {
			cur := maxInFlight.Load()
			if n <= cur || maxInFlight.CompareAndSwap(cur, n) {
				break
			}
		}
		time.Sleep(50 * time.Millisecond) // hold the slot briefly to force contention
		inFlight.Add(-1)
		processed.Add(1)
		return nil
	}

	pool := queue.NewWorkerPool(q, handler, concurrency, testLogger())

	runCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		pool.Run(runCtx)
	}()

	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && processed.Load() < jobCount {
		time.Sleep(50 * time.Millisecond)
	}
	cancel()
	wg.Wait()

	if got := processed.Load(); got != jobCount {
		t.Errorf("processed = %d, want %d", got, jobCount)
	}
	if got := maxInFlight.Load(); got > concurrency {
		t.Errorf("maxInFlight = %d, want <= %d", got, concurrency)
	}
}

func TestWorkerPool_GracefulDrainFinishesInFlightJob(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-pool-drain", queue.Options{VisibilityTimeout: 10 * time.Second, PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	if err := q.Enqueue(ctx, "slow-job", "payload"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	started := make(chan struct{})
	finished := make(chan struct{})

	handler := func(_ context.Context, _ *queue.Job) error {
		close(started)
		time.Sleep(300 * time.Millisecond) // still running when we cancel below
		close(finished)
		return nil
	}

	pool := queue.NewWorkerPool(q, handler, 1, testLogger())

	runCtx, cancel := context.WithCancel(context.Background())

	runDone := make(chan struct{})
	go func() {
		pool.Run(runCtx)
		close(runDone)
	}()

	<-started
	cancel() // signal shutdown while the handler is still mid-flight

	select {
	case <-finished:
	case <-time.After(2 * time.Second):
		t.Fatal("handler never finished - shutdown must not abandon in-flight work")
	}

	select {
	case <-runDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Run() never returned after its in-flight job completed")
	}

	// The job must have been Acked, not left stuck in processing or
	// silently dropped.
	if procLen, _ := q.ProcessingLen(ctx); procLen != 0 {
		t.Errorf("ProcessingLen() = %d, want 0 (the in-flight job should have been acked)", procLen)
	}
}

func TestWorkerPool_NacksOnHandlerError(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-pool-nack", queue.Options{MaxAttempts: 1, VisibilityTimeout: 5 * time.Second, PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	if err := q.Enqueue(ctx, "bad-job", "payload"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	handler := func(_ context.Context, _ *queue.Job) error {
		return fmt.Errorf("handler failure")
	}

	pool := queue.NewWorkerPool(q, handler, 1, testLogger())

	runCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	pool.Run(runCtx)

	if dlLen, _ := q.DeadLetterLen(ctx); dlLen != 1 {
		t.Errorf("DeadLetterLen() = %d, want 1 (MaxAttempts=1, single failure exhausts it)", dlLen)
	}
}
