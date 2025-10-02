package localcache

import (
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
)

// TestLRUCache_EvictionNeverReclaimsReferencedArtifacts drives the cache
// well past its ceiling with a mix of referenced and unreferenced
// artifacts, asserting throughout that every referenced artifact survives
// - both its cache entry and its underlying file - and that eviction
// pressure only ever reclaims unreferenced entries.
func TestLRUCache_EvictionNeverReclaimsReferencedArtifacts(t *testing.T) {
	dir := t.TempDir()
	const entrySize = 100
	const capacity = 5 * entrySize // room for 5 entries at a time
	c := NewLRUCache(capacity)

	// The first five entries simulate artifacts backing live allocations:
	// referenced and expected to survive every later eviction.
	referenced := make(map[string]string) // key -> path
	for i := 0; i < 5; i++ {
		key := fmt.Sprintf("live-%d", i)
		path := touchFile(t, dir, key)
		mustPut(t, c, key, path, entrySize)
		if err := c.Acquire(key); err != nil {
			t.Fatalf("Acquire(%s) error = %v", key, err)
		}
		referenced[key] = path
	}

	// Now push 50 more unreferenced entries through a cache with no spare
	// capacity for them alongside the five referenced ones. Each Put must
	// either evict an older unreferenced entry or fail outright (capacity
	// is fully consumed by referenced entries) - it must never evict a
	// referenced one.
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("cold-%d", i)
		path := touchFile(t, dir, key)

		err := c.Put(key, path, entrySize)
		if err != nil && !errors.Is(err, ErrInsufficientCapacity) {
			t.Fatalf("Put(%s) unexpected error = %v", key, err)
		}

		// Invariant check after every single Put: every referenced
		// artifact must still be retrievable, and its file must still
		// exist on disk.
		for liveKey, livePath := range referenced {
			gotPath, err := c.Get(liveKey)
			if err != nil {
				t.Fatalf("after Put(%s): Get(%s) error = %v, want the referenced entry to survive", key, liveKey, err)
			}
			if gotPath != livePath {
				t.Fatalf("after Put(%s): Get(%s) = %q, want %q", key, liveKey, gotPath, livePath)
			}
			if _, statErr := os.Stat(livePath); statErr != nil {
				t.Fatalf("after Put(%s): referenced file %s was deleted", key, livePath)
			}
		}
	}

	// Since the five referenced entries fully consume capacity, no cold
	// entry could ever have been admitted - confirm the cache holds
	// exactly the five referenced entries and nothing else.
	if c.Len() != 5 {
		t.Errorf("Len() = %d, want 5 (only the referenced entries should remain)", c.Len())
	}
}

// TestLRUCache_ReleasedArtifactsAreReclaimedOnlyOnceUnreferenced verifies
// the companion invariant: once every reference to an artifact is
// released, it becomes eligible for reclamation, but not before, and not
// automatically - only under actual capacity pressure.
func TestLRUCache_ReleasedArtifactsAreReclaimedOnlyOnceUnreferenced(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(300) // room for 3 entries

	pathA := touchFile(t, dir, "a")
	pathB := touchFile(t, dir, "b")
	pathC := touchFile(t, dir, "c")
	pathD := touchFile(t, dir, "d")

	mustPut(t, c, "a", pathA, 100)
	mustPut(t, c, "b", pathB, 100)
	mustPut(t, c, "c", pathC, 100)

	for _, key := range []string{"a", "b", "c"} {
		if err := c.Acquire(key); err != nil {
			t.Fatalf("Acquire(%s) error = %v", key, err)
		}
	}

	// Fully referenced: a fourth entry cannot be admitted at all.
	if err := c.Put("d", pathD, 100); !errors.Is(err, ErrInsufficientCapacity) {
		t.Fatalf("Put(d) error = %v, want ErrInsufficientCapacity while a, b, c are all referenced", err)
	}

	// Releasing "b" (but not a or c) should let "d" be admitted by
	// evicting "b" specifically.
	if err := c.Release("b"); err != nil {
		t.Fatalf("Release(b) error = %v", err)
	}
	if err := c.Put("d", pathD, 100); err != nil {
		t.Fatalf("Put(d) error = %v, want nil now that b is releasable", err)
	}

	if _, err := c.Get("b"); !errors.Is(err, ErrNotFound) {
		t.Error("b should have been evicted once its reference count returned to zero")
	}
	if _, statErr := os.Stat(pathB); !os.IsNotExist(statErr) {
		t.Error("b's file should have been deleted on eviction")
	}
	for _, key := range []string{"a", "c", "d"} {
		if _, err := c.Get(key); err != nil {
			t.Errorf("Get(%s) error = %v, want nil", key, err)
		}
	}
}

// TestLRUCache_ConcurrentChurnNeverLosesAReferencedArtifact hammers the
// cache with concurrent Put/Acquire/Release/Get traffic while a permanent
// "canary" artifact stays referenced throughout, asserting under -race
// that the canary is never evicted despite constant capacity pressure
// from everything else.
func TestLRUCache_ConcurrentChurnNeverLosesAReferencedArtifact(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(1000) // room for ~10 entries at a time

	canaryPath := touchFile(t, dir, "canary")
	mustPut(t, c, "canary", canaryPath, 100)
	if err := c.Acquire("canary"); err != nil {
		t.Fatalf("Acquire(canary) error = %v", err)
	}

	var wg sync.WaitGroup
	const workers = 8
	const opsPerWorker = 200

	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			for i := 0; i < opsPerWorker; i++ {
				key := fmt.Sprintf("churn-%d-%d", worker, i)
				path := touchFile(t, dir, key)

				_ = c.Put(key, path, 100) // capacity errors are expected and fine

				if i%3 == 0 {
					_ = c.Acquire(key)
					_, _ = c.Get("canary")
					_ = c.Release(key)
				}
			}
		}(w)
	}
	wg.Wait()

	if _, err := c.Get("canary"); err != nil {
		t.Fatalf("Get(canary) error = %v, want the referenced canary to have survived all concurrent churn", err)
	}
	if _, statErr := os.Stat(canaryPath); statErr != nil {
		t.Fatalf("canary file was deleted despite remaining referenced throughout: %v", statErr)
	}
}
