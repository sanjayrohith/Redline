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

// ErrGPUZeroingInProgress means the requested device's previous tenant
// has released it, but its VRAM zeroing pass has not yet completed - the
// device is claimable by no one, including its previous tenant, until
// CompleteZero runs.
var ErrGPUZeroingInProgress = errors.New("scheduler: gpu vram zeroing in progress, device not yet reallocatable")

// bindScript atomically claims a GPU device for one tenant: it succeeds
// immediately if the device is unbound or already bound to the same
// tenant (idempotent re-bind, e.g. a scheduler retry), and refuses -
// without touching the existing binding - if it belongs to a different
// tenant or is still mid-zeroing. This has to be a single round trip: a
// GET-then-SET from Go would let two concurrent placements onto the same
// freshly-freed device both observe "unbound" and both believe they won it.
const bindScript = `
local key = KEYS[1]
local tenant_id = ARGV[1]
local allocation_id = ARGV[2]

if redis.call('HGET', key, 'zeroing') == '1' then
	return {0, 'zeroing', ''}
end

local existing = redis.call('HGET', key, 'tenant_id')
if existing and existing ~= tenant_id then
	local existing_alloc = redis.call('HGET', key, 'allocation_id')
	return {0, existing, existing_alloc}
end

redis.call('HSET', key, 'tenant_id', tenant_id, 'allocation_id', allocation_id)
redis.call('HDEL', key, 'zeroing')
return {1, tenant_id, allocation_id}
`

// releaseScript, called when an allocation gives up a device, does not
// free it outright: it clears the tenant binding but marks the device
// 'zeroing', so bindScript refuses every claim - including one from the
// very tenant that just released it - until CompleteZero runs. This is
// what actually blocks reallocation on the zero pass; without it,
// release-then-immediately-rebind would race the zeroing pass and could
// hand a still-dirty device to the next tenant.
const releaseScript = `
local key = KEYS[1]
local allocation_id = ARGV[1]

local existing_alloc = redis.call('HGET', key, 'allocation_id')
if existing_alloc == allocation_id then
	redis.call('HSET', key, 'zeroing', '1')
	redis.call('HDEL', key, 'tenant_id', 'allocation_id')
	return 1
end
return 0
`

// completeZeroScript deletes the binding record entirely once its zero
// pass has finished, making the device claimable by any tenant again.
const completeZeroScript = `
local key = KEYS[1]
if redis.call('HGET', key, 'zeroing') == '1' then
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
	if existingTenant == "zeroing" {
		return fmt.Errorf("%w: device %s", ErrGPUZeroingInProgress, deviceID)
	}
	existingAllocation, _ := fields[2].(string)
	return fmt.Errorf("%w: device %s is bound to tenant %s (allocation %s)",
		ErrGPUBoundToOtherTenant, deviceID, existingTenant, existingAllocation)
}

// Release ends allocationID's binding of deviceID, if it still holds it,
// and marks the device as pending VRAM zeroing: it is not reallocatable
// to anyone, including the releasing tenant, until CompleteZero runs.
func (g *GPUBinding) Release(ctx context.Context, deviceID, allocationID string) error {
	if err := g.client.Eval(ctx, releaseScript, []string{gpuBindingKey(deviceID)}, allocationID).Err(); err != nil {
		return fmt.Errorf("scheduler: release gpu %s from allocation %s: %w", deviceID, allocationID, err)
	}
	return nil
}

// ZeroFunc performs the actual VRAM zeroing pass against a physical
// device - in production, an out-of-band pass that clears every byte of
// the device's memory (mitigating LeftoverLocals-class cross-tenant
// leakage) before the next tenant's weights are loaded into it.
type ZeroFunc func(ctx context.Context, deviceID string) error

// ZeroDevice runs zero against deviceID and, only on success, clears its
// zeroing hold so it becomes claimable again. A failed pass leaves the
// hold in place: Bind keeps refusing the device rather than risk handing
// out VRAM that was never actually cleared.
func (g *GPUBinding) ZeroDevice(ctx context.Context, deviceID string, zero ZeroFunc) error {
	if err := zero(ctx, deviceID); err != nil {
		return fmt.Errorf("scheduler: zero vram for gpu %s: %w", deviceID, err)
	}
	return g.CompleteZero(ctx, deviceID)
}

// CompleteZero clears deviceID's zeroing hold, making it claimable by any
// tenant again. Callers normally reach this through ZeroDevice; it is
// exported directly for tests and for a caller that already ran its own
// zero pass out of band.
func (g *GPUBinding) CompleteZero(ctx context.Context, deviceID string) error {
	if err := g.client.Eval(ctx, completeZeroScript, []string{gpuBindingKey(deviceID)}).Err(); err != nil {
		return fmt.Errorf("scheduler: complete vram zero for gpu %s: %w", deviceID, err)
	}
	return nil
}

// TenantFor returns the tenant currently bound to deviceID, and whether
// any binding exists at all. It reports no tenant while the device is
// mid-zeroing, even though the record still exists - see IsZeroing.
func (g *GPUBinding) TenantFor(ctx context.Context, deviceID string) (string, bool, error) {
	fields, err := g.client.HGetAll(ctx, gpuBindingKey(deviceID)).Result()
	if err != nil {
		return "", false, fmt.Errorf("scheduler: read gpu %s binding: %w", deviceID, err)
	}
	tenantID, ok := fields["tenant_id"]
	return tenantID, ok, nil
}

// IsZeroing reports whether deviceID is currently blocked on a pending
// VRAM zeroing pass.
func (g *GPUBinding) IsZeroing(ctx context.Context, deviceID string) (bool, error) {
	fields, err := g.client.HGetAll(ctx, gpuBindingKey(deviceID)).Result()
	if err != nil {
		return false, fmt.Errorf("scheduler: read gpu %s binding: %w", deviceID, err)
	}
	return fields["zeroing"] == "1", nil
}
