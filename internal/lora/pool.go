package lora

import (
	"context"
	"fmt"
	"sync"
)

// LoadFunc and UnloadFunc perform the actual hot-swap against a live
// engine - vLLM's /v1/load_lora_adapter and /v1/unload_lora_adapter,
// which mutate a running engine's resident adapter set without
// restarting it or reloading the base model. Pool takes them as
// injected dependencies so its eviction policy is independently
// testable without a live engine.
type LoadFunc func(ctx context.Context, adapterName string) error

// UnloadFunc is the counterpart hot-swap operation LoadFunc performs.
type UnloadFunc func(ctx context.Context, adapterName string) error

// Pool manages which adapters are resident on a live engine, bounded to
// maxSlots concurrently loaded at once. When a request needs an adapter
// that is not resident and the pool is already full, Pool evicts the
// least recently used adapter to make room - the same policy an OS page
// cache uses, applied to GPU-resident LoRA weights instead of pages.
type Pool struct {
	maxSlots int
	load     LoadFunc
	unload   UnloadFunc
	router   *Router

	mu    sync.Mutex
	order []string // least-recently-used first, most-recently-used last
}

// NewPool returns a Pool bounded to maxSlots concurrently resident
// adapters, keeping router's resident set in sync with every load and
// eviction.
func NewPool(maxSlots int, router *Router, load LoadFunc, unload UnloadFunc) *Pool {
	return &Pool{maxSlots: maxSlots, router: router, load: load, unload: unload}
}

// Acquire ensures adapterName is resident on the engine: a no-op beyond
// marking it most-recently-used if it already is, otherwise hot-loading
// it - evicting the least recently used adapter first if the pool is
// already at its slot limit.
func (p *Pool) Acquire(ctx context.Context, adapterName string) error {
	if p.maxSlots <= 0 {
		return fmt.Errorf("lora: pool has no slots configured (maxSlots=%d)", p.maxSlots)
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	if idx := indexOf(p.order, adapterName); idx >= 0 {
		p.order = append(p.order[:idx], p.order[idx+1:]...)
		p.order = append(p.order, adapterName)
		return nil
	}

	if len(p.order) >= p.maxSlots {
		lru := p.order[0]
		if err := p.unload(ctx, lru); err != nil {
			return fmt.Errorf("lora: evict lru adapter %q to make room for %q: %w", lru, adapterName, err)
		}
		p.router.MarkUnloaded(lru)
		p.order = p.order[1:]
	}

	if err := p.load(ctx, adapterName); err != nil {
		return fmt.Errorf("lora: load adapter %q: %w", adapterName, err)
	}
	p.router.MarkLoaded(adapterName)
	p.order = append(p.order, adapterName)
	return nil
}

// Resident returns the adapters currently loaded, least recently used
// first.
func (p *Pool) Resident() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.order...)
}

func indexOf(s []string, v string) int {
	for i, e := range s {
		if e == v {
			return i
		}
	}
	return -1
}
