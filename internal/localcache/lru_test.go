package localcache

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func touchFile(t *testing.T, dir, name string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte("data"), 0o600); err != nil {
		t.Fatalf("write fixture file: %v", err)
	}
	return path
}

func TestLRUCache_PutAndGet(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(1000)

	path := touchFile(t, dir, "a")
	if err := c.Put("a", path, 100); err != nil {
		t.Fatalf("Put() error = %v", err)
	}

	got, err := c.Get("a")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if got != path {
		t.Errorf("Get() = %q, want %q", got, path)
	}
	if c.UsedBytes() != 100 {
		t.Errorf("UsedBytes() = %d, want 100", c.UsedBytes())
	}
}

func TestLRUCache_GetMissing(t *testing.T) {
	c := NewLRUCache(1000)
	if _, err := c.Get("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("error = %v, want ErrNotFound", err)
	}
}

func TestLRUCache_EvictsLeastRecentlyUsed(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(250)

	pathA := touchFile(t, dir, "a")
	pathB := touchFile(t, dir, "b")
	pathC := touchFile(t, dir, "c")

	mustPut(t, c, "a", pathA, 100)
	mustPut(t, c, "b", pathB, 100)

	// Touch "a" so "b" becomes the least-recently-used entry.
	if _, err := c.Get("a"); err != nil {
		t.Fatalf("Get(a) error = %v", err)
	}

	// Adding "c" (100 bytes) exceeds 250 with all three present (300 >
	// 250), so the LRU entry - "b" - must be evicted, not "a".
	mustPut(t, c, "c", pathC, 100)

	if _, err := c.Get("b"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get(b) error = %v, want ErrNotFound (b should have been evicted)", err)
	}
	if _, err := os.Stat(pathB); !os.IsNotExist(err) {
		t.Error("evicted entry's file should have been deleted from disk")
	}

	if _, err := c.Get("a"); err != nil {
		t.Errorf("Get(a) error = %v, want nil (a was recently touched, should survive)", err)
	}
	if _, err := c.Get("c"); err != nil {
		t.Errorf("Get(c) error = %v, want nil", err)
	}
}

func TestLRUCache_ReferencedEntrySurvivesEvictionPressure(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(150)

	pathA := touchFile(t, dir, "a")
	pathB := touchFile(t, dir, "b")

	mustPut(t, c, "a", pathA, 100)
	if err := c.Acquire("a"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	// "b" alone doesn't exceed capacity, but a+b would (200 > 150). Since
	// "a" is referenced, "b" must simply fail to fit rather than evicting "a".
	if err := c.Put("b", pathB, 100); !errors.Is(err, ErrInsufficientCapacity) {
		t.Errorf("Put(b) error = %v, want ErrInsufficientCapacity", err)
	}

	if _, err := c.Get("a"); err != nil {
		t.Errorf("Get(a) error = %v, want nil (referenced entry must survive)", err)
	}
	if _, err := os.Stat(pathA); err != nil {
		t.Error("referenced entry's file should not have been deleted")
	}
}

func TestLRUCache_ReleaseToZeroAllowsEviction(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(150)

	pathA := touchFile(t, dir, "a")
	pathB := touchFile(t, dir, "b")

	mustPut(t, c, "a", pathA, 100)
	if err := c.Acquire("a"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := c.Release("a"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}

	if err := c.Put("b", pathB, 100); err != nil {
		t.Fatalf("Put(b) error = %v, want nil now that a has been released to zero", err)
	}
	if _, err := c.Get("a"); !errors.Is(err, ErrNotFound) {
		t.Error("a should have been evicted after its reference count returned to zero")
	}
}

func TestLRUCache_Remove(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(1000)

	path := touchFile(t, dir, "a")
	mustPut(t, c, "a", path, 100)

	if err := c.Remove("a"); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("Remove() should delete the underlying file")
	}
	if c.UsedBytes() != 0 {
		t.Errorf("UsedBytes() = %d, want 0", c.UsedBytes())
	}
}

func TestLRUCache_PutReplacesExistingKey(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(1000)

	pathA1 := touchFile(t, dir, "a1")
	pathA2 := touchFile(t, dir, "a2")

	mustPut(t, c, "a", pathA1, 100)
	mustPut(t, c, "a", pathA2, 200)

	if got := c.UsedBytes(); got != 200 {
		t.Errorf("UsedBytes() = %d, want 200 (old entry replaced, not accumulated)", got)
	}
	if c.Len() != 1 {
		t.Errorf("Len() = %d, want 1", c.Len())
	}
}

func TestLRUCache_UnboundedWhenMaxBytesZero(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(0)

	for i := 0; i < 5; i++ {
		path := touchFile(t, dir, string(rune('a'+i)))
		mustPut(t, c, string(rune('a'+i)), path, 1_000_000)
	}
	if c.Len() != 5 {
		t.Errorf("Len() = %d, want 5 (no eviction with maxBytes <= 0)", c.Len())
	}
}

func TestLRUCache_MultipleAcquiresRequireMatchingReleases(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(150)

	pathA := touchFile(t, dir, "a")
	pathB := touchFile(t, dir, "b")

	mustPut(t, c, "a", pathA, 100)
	// Two independent deployments both reference "a".
	if err := c.Acquire("a"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}
	if err := c.Acquire("a"); err != nil {
		t.Fatalf("Acquire() error = %v", err)
	}

	if count, err := c.RefCount("a"); err != nil || count != 2 {
		t.Fatalf("RefCount() = (%d, %v), want (2, nil)", count, err)
	}

	// Releasing once still leaves one live reference, so "a" must survive
	// eviction pressure from "b".
	if err := c.Release("a"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := c.Put("b", pathB, 100); !errors.Is(err, ErrInsufficientCapacity) {
		t.Errorf("Put(b) error = %v, want ErrInsufficientCapacity (a still has one reference)", err)
	}

	// Releasing the final reference makes "a" evictable again.
	if err := c.Release("a"); err != nil {
		t.Fatalf("Release() error = %v", err)
	}
	if err := c.Put("b", pathB, 100); err != nil {
		t.Fatalf("Put(b) error = %v, want nil once a's last reference is released", err)
	}
}

func TestLRUCache_ReleaseWithoutAcquireFails(t *testing.T) {
	dir := t.TempDir()
	c := NewLRUCache(1000)

	path := touchFile(t, dir, "a")
	mustPut(t, c, "a", path, 100)

	if err := c.Release("a"); !errors.Is(err, ErrNotReferenced) {
		t.Errorf("Release() error = %v, want ErrNotReferenced", err)
	}
}

func TestLRUCache_AcquireReleaseMissingKey(t *testing.T) {
	c := NewLRUCache(1000)

	if err := c.Acquire("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Acquire() error = %v, want ErrNotFound", err)
	}
	if err := c.Release("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Release() error = %v, want ErrNotFound", err)
	}
	if _, err := c.RefCount("missing"); !errors.Is(err, ErrNotFound) {
		t.Errorf("RefCount() error = %v, want ErrNotFound", err)
	}
}

func mustPut(t *testing.T, c *LRUCache, key, path string, size int64) {
	t.Helper()
	if err := c.Put(key, path, size); err != nil {
		t.Fatalf("Put(%s) error = %v", key, err)
	}
}
