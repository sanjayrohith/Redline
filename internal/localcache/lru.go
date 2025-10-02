// Package localcache maintains the worker node's on-disk NVMe artifact
// cache: a size-bounded store with least-recently-used eviction that
// never reclaims an artifact currently backing a live allocation.
package localcache

import (
	"container/list"
	"errors"
	"fmt"
	"os"
	"sync"
)

// ErrInsufficientCapacity is returned when an incoming entry cannot fit
// even after evicting every zero-reference entry - the referenced (in-use)
// entries alone already consume too much of the configured capacity.
var ErrInsufficientCapacity = errors.New("localcache: insufficient capacity after evicting all unreferenced entries")

// ErrNotFound is returned when a key has no cached entry.
var ErrNotFound = errors.New("localcache: entry not found")

// ErrNotReferenced is returned by Release when key's reference count is
// already zero - a caller bug, since every Release should pair with a
// prior successful Acquire.
var ErrNotReferenced = errors.New("localcache: release called with no matching acquire")

type entry struct {
	key      string
	path     string
	size     int64
	refCount int
}

// LRUCache is a size-bounded, least-recently-used on-disk cache with
// reference counting: an entry backing one or more live deployments
// (refCount > 0) is never chosen for eviction, no matter how cold it is.
type LRUCache struct {
	mu        sync.Mutex
	maxBytes  int64
	usedBytes int64
	order     *list.List // front = most recently used
	elements  map[string]*list.Element
}

// NewLRUCache returns an LRUCache bounded to maxBytes total entry size.
func NewLRUCache(maxBytes int64) *LRUCache {
	return &LRUCache{
		maxBytes: maxBytes,
		order:    list.New(),
		elements: make(map[string]*list.Element),
	}
}

// Put registers path (size bytes) under key, evicting least-recently-used
// zero-reference entries as needed to stay within capacity. If key
// already exists, its entry is replaced (its reference count reset to 0)
// and moved to most-recently-used.
func (c *LRUCache) Put(key, path string, size int64) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, ok := c.elements[key]; ok {
		c.removeElement(el, false)
	}

	if err := c.evictUntilFits(size); err != nil {
		return err
	}

	el := c.order.PushFront(&entry{key: key, path: path, size: size})
	c.elements[key] = el
	c.usedBytes += size

	return nil
}

// evictUntilFits evicts zero-reference entries, least-recently-used
// first, until incomingSize more bytes fit within maxBytes. Caller holds c.mu.
func (c *LRUCache) evictUntilFits(incomingSize int64) error {
	if c.maxBytes <= 0 {
		return nil // unbounded
	}

	for c.usedBytes+incomingSize > c.maxBytes {
		victim := c.leastRecentlyUsedUnreferenced()
		if victim == nil {
			return fmt.Errorf("%w: need %d more bytes, %d already referenced of %d capacity",
				ErrInsufficientCapacity, incomingSize-(c.maxBytes-c.usedBytes), c.usedBytes, c.maxBytes)
		}
		c.removeElement(victim, true)
	}

	return nil
}

func (c *LRUCache) leastRecentlyUsedUnreferenced() *list.Element {
	for el := c.order.Back(); el != nil; el = el.Prev() {
		if el.Value.(*entry).refCount == 0 {
			return el
		}
	}
	return nil
}

// removeElement removes el from the cache's bookkeeping. If deleteFile is
// true, the underlying file at its path is also removed from disk.
func (c *LRUCache) removeElement(el *list.Element, deleteFile bool) {
	e := el.Value.(*entry)
	c.order.Remove(el)
	delete(c.elements, e.key)
	c.usedBytes -= e.size

	if deleteFile {
		_ = os.Remove(e.path)
	}
}

// Get returns key's path and marks it most-recently-used.
func (c *LRUCache) Get(key string) (string, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return "", ErrNotFound
	}
	c.order.MoveToFront(el)
	return el.Value.(*entry).path, nil
}

// Acquire increments key's reference count, marking one more live
// deployment as backed by this artifact. A referenced entry (refCount >
// 0) is never chosen for eviction.
func (c *LRUCache) Acquire(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return ErrNotFound
	}
	el.Value.(*entry).refCount++
	return nil
}

// Release decrements key's reference count. Once it reaches zero, the
// entry becomes eligible for eviction again, though it is not evicted
// immediately - only when capacity pressure requires it.
func (c *LRUCache) Release(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return ErrNotFound
	}
	e := el.Value.(*entry)
	if e.refCount == 0 {
		return ErrNotReferenced
	}
	e.refCount--
	return nil
}

// RefCount returns key's current reference count.
func (c *LRUCache) RefCount(key string) (int, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return 0, ErrNotFound
	}
	return el.Value.(*entry).refCount, nil
}

// Remove explicitly evicts key and deletes its underlying file,
// regardless of reference count - used when an artifact is known bad
// (e.g. a checksum mismatch discovered after caching) rather than simply cold.
func (c *LRUCache) Remove(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return ErrNotFound
	}
	c.removeElement(el, true)
	return nil
}

// UsedBytes returns the current total size of every cached entry.
func (c *LRUCache) UsedBytes() int64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.usedBytes
}

// Len returns the number of cached entries.
func (c *LRUCache) Len() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.order.Len()
}
