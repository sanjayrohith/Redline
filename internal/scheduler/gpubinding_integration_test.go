package scheduler_test

import (
	"context"
	"errors"
	"testing"

	"github.com/sanjayrohith/redline/internal/scheduler"
)

func TestGPUBinding_BindClaimsAnUnboundDevice(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	tenant, ok, err := binding.TenantFor(ctx, "gpu-0")
	if err != nil {
		t.Fatalf("TenantFor() error = %v", err)
	}
	if !ok || tenant != "tenant-a" {
		t.Errorf("TenantFor() = (%q, %v), want (tenant-a, true)", tenant, ok)
	}
}

func TestGPUBinding_BindRefusesADeviceBoundToAnotherTenant(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	err := binding.Bind(ctx, "gpu-0", "tenant-b", "alloc-2")
	if !errors.Is(err, scheduler.ErrGPUBoundToOtherTenant) {
		t.Fatalf("Bind() error = %v, want ErrGPUBoundToOtherTenant", err)
	}

	tenant, _, terr := binding.TenantFor(ctx, "gpu-0")
	if terr != nil {
		t.Fatalf("TenantFor() error = %v", terr)
	}
	if tenant != "tenant-a" {
		t.Errorf("TenantFor() = %q, want the original binding (tenant-a) to survive the refused claim", tenant)
	}
}

func TestGPUBinding_BindIsIdempotentForTheSameTenant(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("first Bind() error = %v", err)
	}
	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("re-Bind() by the same tenant/allocation error = %v, want no error", err)
	}
}

func TestGPUBinding_ReleaseBlocksReallocationUntilZeroed(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if err := binding.Release(ctx, "gpu-0", "alloc-1"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	// Neither a new tenant nor the releasing tenant can claim the device
	// while its VRAM zeroing pass is still pending.
	if err := binding.Bind(ctx, "gpu-0", "tenant-b", "alloc-2"); !errors.Is(err, scheduler.ErrGPUZeroingInProgress) {
		t.Fatalf("Bind() by a new tenant before zeroing error = %v, want ErrGPUZeroingInProgress", err)
	}
	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-3"); !errors.Is(err, scheduler.ErrGPUZeroingInProgress) {
		t.Fatalf("Bind() by the releasing tenant before zeroing error = %v, want ErrGPUZeroingInProgress", err)
	}

	zeroing, err := binding.IsZeroing(ctx, "gpu-0")
	if err != nil {
		t.Fatalf("IsZeroing() error = %v", err)
	}
	if !zeroing {
		t.Error("IsZeroing() = false, want true after Release")
	}

	if err := binding.CompleteZero(ctx, "gpu-0"); err != nil {
		t.Fatalf("CompleteZero() error = %v", err)
	}

	if err := binding.Bind(ctx, "gpu-0", "tenant-b", "alloc-2"); err != nil {
		t.Fatalf("Bind() after CompleteZero() error = %v, want the device to be claimable", err)
	}
}

func TestGPUBinding_ZeroDeviceRunsThePassThenFreesTheDevice(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if err := binding.Release(ctx, "gpu-0", "alloc-1"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	var zeroedDevice string
	err := binding.ZeroDevice(ctx, "gpu-0", func(_ context.Context, deviceID string) error {
		zeroedDevice = deviceID
		return nil
	})
	if err != nil {
		t.Fatalf("ZeroDevice() error = %v", err)
	}
	if zeroedDevice != "gpu-0" {
		t.Errorf("zero func received deviceID = %q, want gpu-0", zeroedDevice)
	}

	if err := binding.Bind(ctx, "gpu-0", "tenant-b", "alloc-2"); err != nil {
		t.Fatalf("Bind() after ZeroDevice() error = %v, want the device to be claimable", err)
	}
}

func TestGPUBinding_ZeroDeviceLeavesTheHoldInPlaceOnFailure(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()
	zeroErr := errors.New("zero pass failed")

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if err := binding.Release(ctx, "gpu-0", "alloc-1"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	err := binding.ZeroDevice(ctx, "gpu-0", func(context.Context, string) error {
		return zeroErr
	})
	if !errors.Is(err, zeroErr) {
		t.Fatalf("ZeroDevice() error = %v, want it to wrap %v", err, zeroErr)
	}

	if err := binding.Bind(ctx, "gpu-0", "tenant-b", "alloc-2"); !errors.Is(err, scheduler.ErrGPUZeroingInProgress) {
		t.Fatalf("Bind() after a failed zero pass error = %v, want ErrGPUZeroingInProgress", err)
	}
}

func TestGPUBinding_ReleaseIgnoresAStaleAllocation(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}

	// A stale release from an allocation that never actually won the
	// device (or was already superseded) must not evict the live binding.
	if err := binding.Release(ctx, "gpu-0", "alloc-stale"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	tenant, ok, err := binding.TenantFor(ctx, "gpu-0")
	if err != nil {
		t.Fatalf("TenantFor() error = %v", err)
	}
	if !ok || tenant != "tenant-a" {
		t.Errorf("TenantFor() = (%q, %v), want the live binding (tenant-a, true) to survive a stale release", tenant, ok)
	}
}
