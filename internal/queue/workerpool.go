package queue

import (
	"context"
	"errors"
	"log/slog"
	"sync"
)

// Handler processes one dequeued job. A nil return Acks the job; a
// non-nil return Nacks it (requeue or dead-letter, per the queue's
// MaxAttempts).
type Handler func(ctx context.Context, job *Job) error

// WorkerPool runs a bounded number of concurrent consumers against a
// Queue. The concurrency bound should be set to the number of GPUs (or
// other scarce resource) actually available to process jobs with, not an
// arbitrary parallelism target.
type WorkerPool struct {
	queue       *Queue
	handler     Handler
	concurrency int
	logger      *slog.Logger
}

// NewWorkerPool returns a WorkerPool consuming from queue with handler,
// running at most concurrency jobs at once.
func NewWorkerPool(queue *Queue, handler Handler, concurrency int, logger *slog.Logger) *WorkerPool {
	if concurrency <= 0 {
		concurrency = 1
	}
	return &WorkerPool{queue: queue, handler: handler, concurrency: concurrency, logger: logger}
}

// Run starts concurrency workers pulling from the queue and blocks until
// every worker has stopped. Workers stop accepting new jobs once ctx is
// done, but a job already dequeued is always run to completion - via a
// context independent of ctx - and Acked or Nacked before its worker
// exits, so shutdown never abandons in-flight work.
func (p *WorkerPool) Run(ctx context.Context) {
	var wg sync.WaitGroup
	wg.Add(p.concurrency)

	for i := 0; i < p.concurrency; i++ {
		go func(workerID int) {
			defer wg.Done()
			p.workerLoop(ctx, workerID)
		}(i)
	}

	wg.Wait()
}

func (p *WorkerPool) workerLoop(ctx context.Context, workerID int) {
	for {
		job, err := p.queue.Dequeue(ctx)
		if err != nil {
			if !errors.Is(err, ErrEmpty) {
				p.logger.Error("dequeue error", "worker", workerID, "error", err)
			}
			return
		}

		// The job is now ours and invisible to other consumers. Run it
		// to completion on a context independent of the pool's shutdown
		// signal, so a cancelled ctx never abandons in-flight work.
		p.process(context.Background(), workerID, job)
	}
}

func (p *WorkerPool) process(ctx context.Context, workerID int, job *Job) {
	err := p.handler(ctx, job)
	if err == nil {
		if ackErr := p.queue.Ack(ctx, job.ID); ackErr != nil {
			p.logger.Error("ack job", "worker", workerID, "job_id", job.ID, "error", ackErr)
		}
		return
	}

	p.logger.Warn("job handler failed", "worker", workerID, "job_id", job.ID, "error", err)

	result, nackErr := p.queue.Nack(ctx, job.ID)
	if nackErr != nil {
		p.logger.Error("nack job", "worker", workerID, "job_id", job.ID, "error", nackErr)
		return
	}
	if result.DeadLettered {
		p.logger.Error("job exhausted retries, dead-lettered", "worker", workerID, "job_id", job.ID, "attempts", result.Attempts)
	}
}
