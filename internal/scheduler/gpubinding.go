package scheduler

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/v9"
)

// ErrGPUBoundToOtherTenant means the requested device is already
// exclusively bound to a different tenant's allocation.
var ErrGPUBoundToOtherTenant = errors.New("scheduler: gpu already exclusively bound to another tenant")

// bindScript atomically claims a GPU device for one tenant: it succeeds
// immediately if the device is unbound or already bound to the same
// tenant (idempotent re-bind, e.g. a scheduler retry), and refuses -
// without touching the existing binding - if it belongs to a different
// tenant. This has to be a single round trip: a GET-then-SET from Go
// would let two concurrent placements onto the same freshly-freed device
// both observe "unbound" and both believe they won it.
const bindScript = `
local key = KEYS[1]
local tenant_id = ARGV[1]
local allocation_id = ARGV[2]

local existing = redis.call('HGET', key, 'tenant_id')
if existing and existing ~= tenant_id then
	local existing_alloc = redis.call('HGET', key, 'allocation_id')
	return {0, existing, existing_alloc}
end

redis.call('HSET', key, 'tenant_id', tenant_id, 'allocation_id', allocation_id)
return {1, tenant_id, allocation_id}
`

// releaseScript frees a device's binding only if it still belongs to the
// allocation releasing it, so a stale release from an allocation that
// already lost a race (or was already superseded) cannot evict a
// different, still-live binding.
const releaseScript = `
local key = KEYS[1]
local allocation_id = ARGV[1]

local existing_alloc = redis.call('HGET', key, 'allocation_id')
if existing_alloc == allocation_id then
	redis.call('DEL', key)
	return 1
end
return 0
`

// gpuBindingRedisClient is the subset of *redisclient.Client GPUBinding needs.
type gpuBindingRedisClient interface {
	Eval(ctx context.Context, script string, keys []string, args ...any) *redis.Cmd
	HGetAll(ctx context.Context, key string) *redis.MapStringStringCmd
}

// GPUBinding enforces exclusive, one-tenant-at-a-time ownership of each
// physical GPU device for the lifetime of the allocation using it. Unlike
// VRAM-based bin-packing (see SelectNode), this is not a capacity check:
// a device with plenty of free VRAM for a second tenant's workload is
// still refused, because time-slicing compute across tenants on the same
// device undermines the isolation guarantees the rest of the sandbox
// (network namespace, read-only rootfs, dropped capabilities) exists to
// provide.
type GPUBinding struct {
	client gpuBindingRedisClient
}

// NewGPUBinding returns a GPUBinding backed by client.
func NewGPUBinding(client gpuBindingRedisClient) *GPUBinding {
	return &GPUBinding{client: client}
}

func gpuBindingKey(deviceID string) string {
	return "gpu:binding:" + deviceID
}

// Bind exclusively assigns deviceID to tenantID for allocationID's
// lifetime, or returns ErrGPUBoundToOtherTenant if a different tenant
// already holds it.
func (g *GPUBinding) Bind(ctx context.Context, deviceID, tenantID, allocationID string) error {
	res, err := g.client.Eval(ctx, bindScript, []string{gpuBindingKey(deviceID)}, tenantID, allocationID).Result()
	if err != nil {
		return fmt.Errorf("scheduler: bind gpu %s to tenant %s: %w", deviceID, tenantID, err)
	}

	fields, ok := res.([]any)
	if !ok || len(fields) != 3 {
		return fmt.Errorf("scheduler: bind gpu %s: unexpected script result %v", deviceID, res)
	}
	ok64, _ := fields[0].(int64)
	if ok64 == 1 {
		return nil
	}

	existingTenant, _ := fields[1].(string)
	existingAllocation, _ := fields[2].(string)
	return fmt.Errorf("%w: device %s is bound to tenant %s (allocation %s)",
		ErrGPUBoundToOtherTenant, deviceID, existingTenant, existingAllocation)
}

// Release frees deviceID's binding if it still belongs to allocationID.
func (g *GPUBinding) Release(ctx context.Context, deviceID, allocationID string) error {
	if err := g.client.Eval(ctx, releaseScript, []string{gpuBindingKey(deviceID)}, allocationID).Err(); err != nil {
		return fmt.Errorf("scheduler: release gpu %s from allocation %s: %w", deviceID, allocationID, err)
	}
	return nil
}

// TenantFor returns the tenant currently bound to deviceID, and whether
// any binding exists at all.
func (g *GPUBinding) TenantFor(ctx context.Context, deviceID string) (string, bool, error) {
	fields, err := g.client.HGetAll(ctx, gpuBindingKey(deviceID)).Result()
	if err != nil {
		return "", false, fmt.Errorf("scheduler: read gpu %s binding: %w", deviceID, err)
	}
	tenantID, ok := fields["tenant_id"]
	return tenantID, ok, nil
}
