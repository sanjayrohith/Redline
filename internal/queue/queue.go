// Package queue implements a Redis-backed durable job queue with
// reliable-delivery semantics: a dequeued job stays invisible to other
// consumers only until its visibility timeout expires, and a job that
// exhausts its retry budget is moved to a dead-letter list instead of
// being dropped or retried forever.
package queue

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrEmpty is returned by Dequeue when no job became available before ctx
// was done.
var ErrEmpty = errors.New("queue: no job available")

// redisClient is the subset of *redisclient.Client the queue needs.
type redisClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
	ZRangeByScore(ctx context.Context, key string, opt *redis.ZRangeBy) *redis.StringSliceCmd
	HSet(ctx context.Context, key string, values ...any) *redis.IntCmd
	LPush(ctx context.Context, key string, values ...any) *redis.IntCmd
	LLen(ctx context.Context, key string) *redis.IntCmd
}

// Job is one unit of work dequeued from the queue.
type Job struct {
	ID       string
	Payload  string
	Attempts int
}

// Options configures a Queue.
type Options struct {
	// MaxAttempts is how many times a job may be dequeued (including the
	// first) before it is moved to the dead-letter list.
	MaxAttempts int
	// VisibilityTimeout bounds how long a dequeued job stays invisible to
	// other consumers before Reap makes it available again.
	VisibilityTimeout time.Duration
	// PollInterval is how often Dequeue retries an empty queue while ctx
	// is not yet done.
	PollInterval time.Duration
}

func (o Options) normalize() Options {
	if o.MaxAttempts <= 0 {
		o.MaxAttempts = 5
	}
	if o.VisibilityTimeout <= 0 {
		o.VisibilityTimeout = 30 * time.Second
	}
	if o.PollInterval <= 0 {
		o.PollInterval = 200 * time.Millisecond
	}
	return o
}

// Queue is one named durable job queue.
type Queue struct {
	client redisClient
	name   string
	opts   Options
}

// New returns a Queue named name, backed by client.
func New(client redisClient, name string, opts Options) *Queue {
	return &Queue{client: client, name: name, opts: opts.normalize()}
}

func (q *Queue) pendingKey() string      { return fmt.Sprintf("queue:%s:pending", q.name) }
func (q *Queue) processingKey() string   { return fmt.Sprintf("queue:%s:processing", q.name) }
func (q *Queue) visibilityKey() string   { return fmt.Sprintf("queue:%s:visibility", q.name) }
func (q *Queue) deadLetterKey() string   { return fmt.Sprintf("queue:%s:deadletter", q.name) }
func (q *Queue) jobKeyPrefix() string    { return fmt.Sprintf("queue:%s:job:", q.name) }
func (q *Queue) jobKey(id string) string { return q.jobKeyPrefix() + id }

// Enqueue adds a new job with the given id and payload to the pending
// list. id must be unique for the life of the job (a deployment ID or
// similar caller-generated identifier works well).
func (q *Queue) Enqueue(ctx context.Context, id, payload string) error {
	if _, err := q.client.HSet(ctx, q.jobKey(id), "payload", payload, "attempts", 0).Result(); err != nil {
		return fmt.Errorf("queue: store payload for %s: %w", id, err)
	}
	if _, err := q.client.LPush(ctx, q.pendingKey(), id).Result(); err != nil {
		return fmt.Errorf("queue: enqueue %s: %w", id, err)
	}
	return nil
}

// PendingLen returns the number of jobs waiting to be dequeued.
func (q *Queue) PendingLen(ctx context.Context) (int64, error) {
	return q.llen(ctx, q.pendingKey())
}

// ProcessingLen returns the number of jobs currently dequeued and not
// yet acked, nacked, or reaped.
func (q *Queue) ProcessingLen(ctx context.Context) (int64, error) {
	return q.llen(ctx, q.processingKey())
}

// DeadLetterLen returns the number of jobs that exhausted their retries.
func (q *Queue) DeadLetterLen(ctx context.Context) (int64, error) {
	return q.llen(ctx, q.deadLetterKey())
}

func (q *Queue) llen(ctx context.Context, key string) (int64, error) {
	n, err := q.client.LLen(ctx, key).Result()
	if err != nil {
		return 0, fmt.Errorf("queue: length of %s: %w", key, err)
	}
	return n, nil
}
