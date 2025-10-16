package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// ErrNoIdleRecord means no last-request timestamp has ever been recorded
// for this allocation - it either never received a request, or Forget
// was already called for it.
var ErrNoIdleRecord = errors.New("scheduler: no idle record for allocation")

// idleRedisClient is the subset of *redisclient.Client the idle tracker needs.
type idleRedisClient interface {
	Set(ctx context.Context, key string, value any, expiration time.Duration) *redis.StatusCmd
	Get(ctx context.Context, key string) *redis.StringCmd
	Del(ctx context.Context, keys ...string) *redis.IntCmd
}

// IdleTracker records each allocation's last-request time in Redis, so
// both the idle reaper and the telemetry surface can derive how long an
// allocation has sat unused without either one owning that state itself.
type IdleTracker struct {
	client idleRedisClient
}

// NewIdleTracker returns an IdleTracker backed by client.
func NewIdleTracker(client idleRedisClient) *IdleTracker {
	return &IdleTracker{client: client}
}

func idleKey(allocationID string) string {
	return "idle:" + allocationID
}

// Touch records now as allocationID's last-request time.
func (t *IdleTracker) Touch(ctx context.Context, allocationID string) error {
	if err := t.client.Set(ctx, idleKey(allocationID), time.Now().UnixMilli(), 0).Err(); err != nil {
		return fmt.Errorf("scheduler: touch idle record for %s: %w", allocationID, err)
	}
	return nil
}

// IdleDuration returns how long allocationID has sat since its last
// recorded request, or ErrNoIdleRecord if it was never touched.
func (t *IdleTracker) IdleDuration(ctx context.Context, allocationID string) (time.Duration, error) {
	val, err := t.client.Get(ctx, idleKey(allocationID)).Result()
	if errors.Is(err, redis.Nil) {
		return 0, ErrNoIdleRecord
	}
	if err != nil {
		return 0, fmt.Errorf("scheduler: read idle record for %s: %w", allocationID, err)
	}

	lastMs, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("scheduler: parse idle record for %s: %w", allocationID, err)
	}

	return time.Since(time.UnixMilli(lastMs)), nil
}

// Forget removes allocationID's idle record, such as once its allocation
// has been torn down.
func (t *IdleTracker) Forget(ctx context.Context, allocationID string) error {
	if err := t.client.Del(ctx, idleKey(allocationID)).Err(); err != nil {
		return fmt.Errorf("scheduler: forget idle record for %s: %w", allocationID, err)
	}
	return nil
}
