package queue_test

import (
	"context"
	"testing"
	"time"

	tcredis "github.com/testcontainers/testcontainers-go/modules/redis"

	"github.com/sanjayrohith/redline/internal/queue"
	"github.com/sanjayrohith/redline/internal/redisclient"
)

func newTestClient(t *testing.T) *redisclient.Client {
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

func TestQueue_EnqueueDequeueAck(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-basic", queue.Options{MaxAttempts: 3, VisibilityTimeout: time.Second, PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	if err := q.Enqueue(ctx, "job-1", "payload-1"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}
	if job.ID != "job-1" || job.Payload != "payload-1" || job.Attempts != 0 {
		t.Errorf("job = %+v, want ID=job-1 Payload=payload-1 Attempts=0", job)
	}

	if procLen, _ := q.ProcessingLen(ctx); procLen != 1 {
		t.Errorf("ProcessingLen() = %d, want 1", procLen)
	}

	if err := q.Ack(ctx, job.ID); err != nil {
		t.Fatalf("Ack() error = %v", err)
	}
	if procLen, _ := q.ProcessingLen(ctx); procLen != 0 {
		t.Errorf("ProcessingLen() after Ack = %d, want 0", procLen)
	}
}

func TestQueue_DequeueEmptyReturnsErrEmptyOnContextDone(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-empty", queue.Options{PollInterval: 20 * time.Millisecond})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	if _, err := q.Dequeue(ctx); err != queue.ErrEmpty {
		t.Errorf("Dequeue() error = %v, want ErrEmpty", err)
	}
}

func TestQueue_NackRequeuesUntilMaxAttemptsThenDeadLetters(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-nack", queue.Options{MaxAttempts: 2, VisibilityTimeout: time.Second, PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	if err := q.Enqueue(ctx, "job-1", "payload"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}

	result, err := q.Nack(ctx, job.ID)
	if err != nil {
		t.Fatalf("Nack() error = %v", err)
	}
	if result.DeadLettered || result.Attempts != 1 {
		t.Fatalf("first Nack() result = %+v, want retry with Attempts=1", result)
	}

	// Requeued: should be dequeueable again.
	job2, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("second Dequeue() error = %v", err)
	}
	if job2.ID != "job-1" || job2.Attempts != 1 {
		t.Errorf("job2 = %+v, want ID=job-1 Attempts=1", job2)
	}

	result2, err := q.Nack(ctx, job2.ID)
	if err != nil {
		t.Fatalf("second Nack() error = %v", err)
	}
	if !result2.DeadLettered || result2.Attempts != 2 {
		t.Fatalf("second Nack() result = %+v, want dead-lettered with Attempts=2", result2)
	}

	if dlLen, _ := q.DeadLetterLen(ctx); dlLen != 1 {
		t.Errorf("DeadLetterLen() = %d, want 1", dlLen)
	}
	if pendingLen, _ := q.PendingLen(ctx); pendingLen != 0 {
		t.Errorf("PendingLen() = %d, want 0 (job is dead-lettered, not pending)", pendingLen)
	}
}

func TestQueue_ReapRequeuesExpiredVisibility(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-reap", queue.Options{MaxAttempts: 5, VisibilityTimeout: 100 * time.Millisecond, PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	if err := q.Enqueue(ctx, "job-1", "payload"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}

	job, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}
	// Simulate a crashed consumer: never Ack or Nack.

	time.Sleep(200 * time.Millisecond) // let visibility timeout expire

	reaped, err := q.Reap(ctx)
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if reaped != 1 {
		t.Fatalf("Reap() = %d, want 1", reaped)
	}

	requeued, err := q.Dequeue(ctx)
	if err != nil {
		t.Fatalf("Dequeue() after reap error = %v", err)
	}
	if requeued.ID != job.ID || requeued.Attempts != 1 {
		t.Errorf("requeued job = %+v, want ID=%s Attempts=1", requeued, job.ID)
	}
}

func TestQueue_ReapNoExpiredJobsIsNoop(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-reap-noop", queue.Options{VisibilityTimeout: time.Minute})
	ctx := context.Background()

	if err := q.Enqueue(ctx, "job-1", "payload"); err != nil {
		t.Fatalf("Enqueue() error = %v", err)
	}
	if _, err := q.Dequeue(ctx); err != nil {
		t.Fatalf("Dequeue() error = %v", err)
	}

	reaped, err := q.Reap(ctx)
	if err != nil {
		t.Fatalf("Reap() error = %v", err)
	}
	if reaped != 0 {
		t.Errorf("Reap() = %d, want 0 (visibility timeout has not expired)", reaped)
	}
}

func TestQueue_MultipleJobsFIFOOrder(t *testing.T) {
	client := newTestClient(t)
	q := queue.New(client, "test-fifo", queue.Options{PollInterval: 20 * time.Millisecond})
	ctx := context.Background()

	for _, id := range []string{"a", "b", "c"} {
		if err := q.Enqueue(ctx, id, "payload-"+id); err != nil {
			t.Fatalf("Enqueue(%s) error = %v", id, err)
		}
	}

	var order []string
	for i := 0; i < 3; i++ {
		job, err := q.Dequeue(ctx)
		if err != nil {
			t.Fatalf("Dequeue() error = %v", err)
		}
		order = append(order, job.ID)
		if err := q.Ack(ctx, job.ID); err != nil {
			t.Fatalf("Ack() error = %v", err)
		}
	}

	want := []string{"a", "b", "c"}
	for i, id := range want {
		if order[i] != id {
			t.Errorf("order = %v, want %v", order, want)
			break
		}
	}
}
