package scheduler

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/redis/go-redis/v9"
)

// OOMFootprintReductionFactor is how much a deployment's VRAM footprint
// is cut before being requeued after a CUDA out-of-memory failure:
// enough headroom to plausibly clear whatever fragmentation or transient
// allocator overhead triggered the failure (vLLM's own memory profiling
// pass is inherently approximate), without discarding so much that the
// retry serves noticeably degraded context length or batch concurrency
// for no reason.
const OOMFootprintReductionFactor = 0.9

// ReducedFootprintBytes returns originalBytes cut by
// OOMFootprintReductionFactor, for requeuing a deployment that failed
// placement or generation with a CUDA out-of-memory error.
func ReducedFootprintBytes(originalBytes int64) int64 {
	return int64(float64(originalBytes) * OOMFootprintReductionFactor)
}

// vramCeilingRedisClient is the subset of *redisclient.Client
// NodeVRAMCeiling needs.
type vramCeilingRedisClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
	Get(ctx context.Context, key string) *redis.StringCmd
}

// downgradeScript ratchets a node's recorded usable VRAM ceiling
// downward only: a new observation lower than what's already recorded
// replaces it, but a higher one is ignored. A CUDA OOM on real hardware
// means the true usable ceiling is lower than believed - it is never
// evidence the node can suddenly hold more than a previous, harder
// failure already proved it can't.
const downgradeScript = `
local key = KEYS[1]
local observed = tonumber(ARGV[1])

local current = tonumber(redis.call('GET', key))
if current == nil or observed < current then
	redis.call('SET', key, observed)
	return 1
end
return 0
`

// NodeVRAMCeiling records, per node, the usable VRAM ceiling observed
// from real CUDA out-of-memory failures - separate from whatever a
// node's advertised hardware capacity claims, since a CUDA OOM is
// concrete evidence that some of that advertised capacity is not
// actually usable (driver reserve, fragmentation, another process).
type NodeVRAMCeiling struct {
	client vramCeilingRedisClient
}

// NewNodeVRAMCeiling returns a NodeVRAMCeiling backed by client.
func NewNodeVRAMCeiling(client vramCeilingRedisClient) *NodeVRAMCeiling {
	return &NodeVRAMCeiling{client: client}
}

func vramCeilingKey(nodeID string) string {
	return "gpu:vram_ceiling:" + nodeID
}

// Downgrade records observedUsableBytes as nodeID's usable VRAM ceiling,
// if it is lower than whatever ceiling (if any) is already recorded.
func (c *NodeVRAMCeiling) Downgrade(ctx context.Context, nodeID string, observedUsableBytes int64) error {
	if err := c.client.Eval(ctx, downgradeScript, []string{vramCeilingKey(nodeID)}, observedUsableBytes).Err(); err != nil {
		return fmt.Errorf("scheduler: downgrade vram ceiling for node %s: %w", nodeID, err)
	}
	return nil
}

// CeilingFor returns nodeID's recorded usable VRAM ceiling, and whether
// any downgrade has ever been recorded for it. A false return means no
// CUDA OOM has ever been observed on this node - callers should fall
// back to the node's advertised hardware capacity, not treat this as zero.
func (c *NodeVRAMCeiling) CeilingFor(ctx context.Context, nodeID string) (int64, bool, error) {
	val, err := c.client.Get(ctx, vramCeilingKey(nodeID)).Result()
	if err != nil {
		if errors.Is(err, redis.Nil) {
			return 0, false, nil
		}
		return 0, false, fmt.Errorf("scheduler: read vram ceiling for node %s: %w", nodeID, err)
	}

	bytes, err := strconv.ParseInt(val, 10, 64)
	if err != nil {
		return 0, false, fmt.Errorf("scheduler: parse vram ceiling for node %s: %w", nodeID, err)
	}
	return bytes, true, nil
}
