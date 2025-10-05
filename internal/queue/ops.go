package queue

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// dequeueScript atomically moves one job from pending to processing,
// records its visibility deadline, and returns its current payload and
// attempt count - or nothing if pending is empty.
const dequeueScript = `
local id = redis.call('RPOPLPUSH', KEYS[1], KEYS[2])
if not id then
	return false
end
redis.call('ZADD', KEYS[3], ARGV[1], id)
local jobKey = KEYS[4] .. id
local payload = redis.call('HGET', jobKey, 'payload')
local attempts = redis.call('HGET', jobKey, 'attempts')
return {id, payload, attempts}
`

// ackScript removes a completed job from processing bookkeeping entirely.
const ackScript = `
redis.call('LREM', KEYS[1], 0, ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
redis.call('DEL', KEYS[3] .. ARGV[1])
return 1
`

// nackScript removes a job from processing bookkeeping and either
// requeues it (attempts remaining) or moves it to the dead-letter list
// (attempts exhausted), atomically incrementing the attempt counter.
const nackScript = `
redis.call('LREM', KEYS[1], 0, ARGV[1])
redis.call('ZREM', KEYS[2], ARGV[1])
local jobKey = KEYS[5] .. ARGV[1]
local attempts = redis.call('HINCRBY', jobKey, 'attempts', 1)
if attempts >= tonumber(ARGV[2]) then
	redis.call('LPUSH', KEYS[4], ARGV[1])
	return {'deadletter', attempts}
else
	redis.call('LPUSH', KEYS[3], ARGV[1])
	return {'retry', attempts}
end
`

// Dequeue blocks (polling every PollInterval) until a job becomes
// available or ctx is done, returning ErrEmpty if ctx ends first. The
// returned job stays invisible to other consumers until Ack or Nack, or
// until its visibility timeout expires and Reap makes it available again.
func (q *Queue) Dequeue(ctx context.Context) (*Job, error) {
	for {
		select {
		case <-ctx.Done():
			return nil, ErrEmpty
		default:
		}

		job, err := q.tryDequeue(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ErrEmpty
			}
			return nil, err
		}
		if job != nil {
			return job, nil
		}

		select {
		case <-ctx.Done():
			return nil, ErrEmpty
		case <-time.After(q.opts.PollInterval):
		}
	}
}

func (q *Queue) tryDequeue(ctx context.Context) (*Job, error) {
	deadline := time.Now().Add(q.opts.VisibilityTimeout).UnixMilli()

	res, err := q.client.Eval(ctx, dequeueScript,
		[]string{q.pendingKey(), q.processingKey(), q.visibilityKey(), q.jobKeyPrefix()},
		deadline,
	).Result()
	if errors.Is(err, redis.Nil) {
		return nil, nil // empty: the script returned Lua false
	}
	if err != nil {
		return nil, fmt.Errorf("queue: dequeue: %w", err)
	}

	values, ok := res.([]any)
	if !ok {
		return nil, nil // empty
	}

	id, _ := values[0].(string)
	payload, _ := values[1].(string)
	attempts, err := strconv.Atoi(fmt.Sprint(values[2]))
	if err != nil {
		return nil, fmt.Errorf("queue: parse attempts for %s: %w", id, err)
	}

	return &Job{ID: id, Payload: payload, Attempts: attempts}, nil
}

// Ack marks a job successfully processed, removing it entirely.
func (q *Queue) Ack(ctx context.Context, id string) error {
	_, err := q.client.Eval(ctx, ackScript,
		[]string{q.processingKey(), q.visibilityKey(), q.jobKeyPrefix()},
		id,
	).Result()
	if err != nil {
		return fmt.Errorf("queue: ack %s: %w", id, err)
	}
	return nil
}

// NackResult reports what became of a job after Nack or Reap: whether it
// was requeued for another attempt or moved to the dead-letter list.
type NackResult struct {
	DeadLettered bool
	Attempts     int
}

// Nack marks a job failed. If it has exhausted MaxAttempts it moves to
// the dead-letter list; otherwise it is requeued for another attempt.
func (q *Queue) Nack(ctx context.Context, id string) (NackResult, error) {
	res, err := q.client.Eval(ctx, nackScript,
		[]string{q.processingKey(), q.visibilityKey(), q.pendingKey(), q.deadLetterKey(), q.jobKeyPrefix()},
		id, q.opts.MaxAttempts,
	).Result()
	if err != nil {
		return NackResult{}, fmt.Errorf("queue: nack %s: %w", id, err)
	}

	values, ok := res.([]any)
	if !ok || len(values) != 2 {
		return NackResult{}, errors.New("queue: unexpected nack script result")
	}
	outcome, _ := values[0].(string)
	attempts, _ := strconv.Atoi(fmt.Sprint(values[1]))

	return NackResult{DeadLettered: outcome == "deadletter", Attempts: attempts}, nil
}

// Reap finds every job whose visibility timeout has expired without an
// Ack and Nacks each one - requeuing it if attempts remain, or moving it
// to the dead-letter list otherwise. It returns how many jobs it reaped.
func (q *Queue) Reap(ctx context.Context) (int, error) {
	nowMs := strconv.FormatInt(time.Now().UnixMilli(), 10)

	expired, err := q.client.ZRangeByScore(ctx, q.visibilityKey(), &redis.ZRangeBy{
		Min: "-inf",
		Max: nowMs,
	}).Result()
	if err != nil {
		return 0, fmt.Errorf("queue: find expired jobs: %w", err)
	}

	for _, id := range expired {
		if _, err := q.Nack(ctx, id); err != nil {
			return 0, fmt.Errorf("queue: reap %s: %w", id, err)
		}
	}

	return len(expired), nil
}
