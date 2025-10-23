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

func TestGPUBinding_ReleaseFreesTheDeviceForAnotherTenant(t *testing.T) {
	client := newTestRedisClient(t)
	binding := scheduler.NewGPUBinding(client)
	ctx := context.Background()

	if err := binding.Bind(ctx, "gpu-0", "tenant-a", "alloc-1"); err != nil {
		t.Fatalf("Bind() error = %v", err)
	}
	if err := binding.Release(ctx, "gpu-0", "alloc-1"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	if err := binding.Bind(ctx, "gpu-0", "tenant-b", "alloc-2"); err != nil {
		t.Fatalf("Bind() after release error = %v, want the device to be claimable", err)
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
