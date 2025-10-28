package lora_test

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"

	"github.com/sanjayrohith/redline/internal/lora"
)

// spyEngine records load/unload calls in order, standing in for a live
// vLLM engine's /v1/load_lora_adapter and /v1/unload_lora_adapter.
type spyEngine struct {
	mu    sync.Mutex
	calls []string

	failLoad map[string]bool
}

func (s *spyEngine) load(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failLoad[name] {
		return errors.New("engine refused to load " + name)
	}
	s.calls = append(s.calls, "load:"+name)
	return nil
}

func (s *spyEngine) unload(_ context.Context, name string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, "unload:"+name)
	return nil
}

func (s *spyEngine) callLog() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.calls...)
}

func TestPool_AcquireLoadsANewAdapter(t *testing.T) {
	engine := &spyEngine{}
	router := lora.NewRouter("base")
	pool := lora.NewPool(2, router, engine.load, engine.unload)

	if err := pool.Acquire(context.Background(), "adapter-a"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	if got := engine.callLog(); !reflect.DeepEqual(got, []string{"load:adapter-a"}) {
		t.Errorf("engine calls = %v, want [load:adapter-a]", got)
	}
	if _, err := router.Resolve("adapter-a"); err != nil {
		t.Errorf("router.Resolve(adapter-a) error = %v, want nil after Acquire", err)
	}
}

func TestPool_AcquireIsANoOpForAlreadyResidentAdapter(t *testing.T) {
	engine := &spyEngine{}
	router := lora.NewRouter("base")
	pool := lora.NewPool(2, router, engine.load, engine.unload)

	_ = pool.Acquire(context.Background(), "adapter-a")
	_ = pool.Acquire(context.Background(), "adapter-a")

	if got := engine.callLog(); len(got) != 1 {
		t.Errorf("engine calls = %v, want exactly one load", got)
	}
}

func TestPool_EvictsLeastRecentlyUsedWhenFull(t *testing.T) {
	engine := &spyEngine{}
	router := lora.NewRouter("base")
	pool := lora.NewPool(2, router, engine.load, engine.unload)
	ctx := context.Background()

	_ = pool.Acquire(ctx, "adapter-a")
	_ = pool.Acquire(ctx, "adapter-b")
	// adapter-a is now LRU (loaded first, never re-touched).
	if err := pool.Acquire(ctx, "adapter-c"); err != nil {
		t.Fatalf("Acquire(adapter-c) error = %v", err)
	}

	want := []string{"load:adapter-a", "load:adapter-b", "unload:adapter-a", "load:adapter-c"}
	if got := engine.callLog(); !reflect.DeepEqual(got, want) {
		t.Errorf("engine calls = %v, want %v", got, want)
	}

	if _, err := router.Resolve("adapter-a"); !errors.Is(err, lora.ErrAdapterNotLoaded) {
		t.Errorf("router.Resolve(adapter-a) error = %v, want ErrAdapterNotLoaded after eviction", err)
	}
	if _, err := router.Resolve("adapter-c"); err != nil {
		t.Errorf("router.Resolve(adapter-c) error = %v, want nil", err)
	}
}

func TestPool_ReacquiringRefreshesRecencyAndProtectsFromEviction(t *testing.T) {
	engine := &spyEngine{}
	router := lora.NewRouter("base")
	pool := lora.NewPool(2, router, engine.load, engine.unload)
	ctx := context.Background()

	_ = pool.Acquire(ctx, "adapter-a")
	_ = pool.Acquire(ctx, "adapter-b")
	_ = pool.Acquire(ctx, "adapter-a") // touch adapter-a: adapter-b is now LRU
	_ = pool.Acquire(ctx, "adapter-c")

	if _, err := router.Resolve("adapter-b"); !errors.Is(err, lora.ErrAdapterNotLoaded) {
		t.Errorf("router.Resolve(adapter-b) error = %v, want ErrAdapterNotLoaded (adapter-b should have been evicted, not adapter-a)", err)
	}
	if _, err := router.Resolve("adapter-a"); err != nil {
		t.Errorf("router.Resolve(adapter-a) error = %v, want nil (re-touched, should have survived)", err)
	}
}

func TestPool_LoadFailureLeavesRouterUnchanged(t *testing.T) {
	engine := &spyEngine{failLoad: map[string]bool{"adapter-a": true}}
	router := lora.NewRouter("base")
	pool := lora.NewPool(2, router, engine.load, engine.unload)

	err := pool.Acquire(context.Background(), "adapter-a")
	if err == nil {
		t.Fatal("Acquire() error = nil, want the engine's load failure to propagate")
	}
	if _, rerr := router.Resolve("adapter-a"); !errors.Is(rerr, lora.ErrAdapterNotLoaded) {
		t.Errorf("router.Resolve(adapter-a) error = %v, want ErrAdapterNotLoaded after a failed load", rerr)
	}
}

func TestPool_RejectsZeroSlotConfiguration(t *testing.T) {
	router := lora.NewRouter("base")
	pool := lora.NewPool(0, router, func(context.Context, string) error { return nil }, func(context.Context, string) error { return nil })

	if err := pool.Acquire(context.Background(), "adapter-a"); err == nil {
		t.Fatal("Acquire() error = nil, want an error for a pool with zero slots")
	}
}
