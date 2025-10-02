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
// even after evicting every unpinned entry - the pinned (in-use) entries
// alone already consume too much of the configured capacity.
var ErrInsufficientCapacity = errors.New("localcache: insufficient capacity after evicting all unpinned entries")

// ErrNotFound is returned when a key has no cached entry.
var ErrNotFound = errors.New("localcache: entry not found")

type entry struct {
	key    string
	path   string
	size   int64
	pinned bool
}

// LRUCache is a size-bounded, least-recently-used on-disk cache. Pinning
// an entry (Pin) marks it as backing a live allocation; a pinned entry is
// never chosen for eviction, only ever explicitly removed (Unpin then
// evicted later, or Remove).
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
// unpinned entries as needed to stay within capacity. If key already
// exists, its entry is replaced and moved to most-recently-used.
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

// evictUntilFits evicts unpinned entries, least-recently-used first,
// until incomingSize more bytes fit within maxBytes. Caller holds c.mu.
func (c *LRUCache) evictUntilFits(incomingSize int64) error {
	if c.maxBytes <= 0 {
		return nil // unbounded
	}

	for c.usedBytes+incomingSize > c.maxBytes {
		victim := c.leastRecentlyUsedUnpinned()
		if victim == nil {
			return fmt.Errorf("%w: need %d more bytes, %d already pinned of %d capacity",
				ErrInsufficientCapacity, incomingSize-(c.maxBytes-c.usedBytes), c.usedBytes, c.maxBytes)
		}
		c.removeElement(victim, true)
	}

	return nil
}

func (c *LRUCache) leastRecentlyUsedUnpinned() *list.Element {
	for el := c.order.Back(); el != nil; el = el.Prev() {
		if !el.Value.(*entry).pinned {
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

// Pin marks key as backing a live allocation, excluding it from eviction
// until Unpin is called.
func (c *LRUCache) Pin(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return ErrNotFound
	}
	el.Value.(*entry).pinned = true
	return nil
}

// Unpin clears a previous Pin, making key eligible for eviction again.
func (c *LRUCache) Unpin(key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.elements[key]
	if !ok {
		return ErrNotFound
	}
	el.Value.(*entry).pinned = false
	return nil
}

// Remove explicitly evicts key and deletes its underlying file,
// regardless of pin state - used when an artifact is known bad (e.g. a
// checksum mismatch discovered after caching) rather than simply cold.
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
