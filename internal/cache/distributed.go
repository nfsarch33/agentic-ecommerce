package cache

import (
	"context"
	"errors"
	"hash/fnv"
	"sort"
	"sync"
	"time"
)

var ErrCacheMiss = errors.New("cache miss")

type entry struct {
	value   []byte
	expires time.Time
}

func (e entry) expired(now time.Time) bool {
	return !e.expires.IsZero() && now.After(e.expires)
}

// DistributedCache is an in-process sharded cache with TTL support.
type DistributedCache struct {
	mu    sync.RWMutex
	items map[string]entry
}

func NewDistributedCache() *DistributedCache {
	return &DistributedCache{items: make(map[string]entry)}
}

func (c *DistributedCache) Set(_ context.Context, key string, value []byte, ttl time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	e := entry{value: value}
	if ttl > 0 {
		e.expires = time.Now().Add(ttl)
	}
	c.items[key] = e
	return nil
}

func (c *DistributedCache) Get(_ context.Context, key string) ([]byte, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok || e.expired(time.Now()) {
		return nil, ErrCacheMiss
	}
	return e.value, nil
}

func (c *DistributedCache) Invalidate(_ context.Context, key string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.items, key)
	return nil
}

func (c *DistributedCache) InvalidatePattern(_ context.Context, pattern string) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	// Simple prefix match for pattern
	for k := range c.items {
		if matchesPattern(k, pattern) {
			delete(c.items, k)
		}
	}
	return nil
}

func (c *DistributedCache) TTL(_ context.Context, key string) (time.Duration, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	e, ok := c.items[key]
	if !ok {
		return 0, ErrCacheMiss
	}
	if e.expires.IsZero() {
		return -1, nil // no expiry
	}
	remaining := time.Until(e.expires)
	if remaining < 0 {
		return 0, ErrCacheMiss
	}
	return remaining, nil
}

func matchesPattern(key, pattern string) bool {
	if len(pattern) == 0 {
		return true
	}
	if pattern[len(pattern)-1] == '*' {
		prefix := pattern[:len(pattern)-1]
		return len(key) >= len(prefix) && key[:len(prefix)] == prefix
	}
	return key == pattern
}

// ConsistentHash implements a hash ring with virtual nodes.
type ConsistentHash struct {
	mu       sync.RWMutex
	ring     map[uint32]string
	sorted   []uint32
	replicas int
}

func NewConsistentHash(replicas int) *ConsistentHash {
	return &ConsistentHash{
		ring:     make(map[uint32]string),
		replicas: replicas,
	}
}

func (h *ConsistentHash) AddNode(node string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 0; i < h.replicas; i++ {
		key := hashKey(node, i)
		h.ring[key] = node
		h.sorted = append(h.sorted, key)
	}
	sort.Slice(h.sorted, func(i, j int) bool { return h.sorted[i] < h.sorted[j] })
}

func (h *ConsistentHash) RemoveNode(node string) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for i := 0; i < h.replicas; i++ {
		key := hashKey(node, i)
		delete(h.ring, key)
	}
	newSorted := h.sorted[:0]
	for _, k := range h.sorted {
		if _, ok := h.ring[k]; ok {
			newSorted = append(newSorted, k)
		}
	}
	h.sorted = newSorted
}

func (h *ConsistentHash) GetNode(key string) string {
	h.mu.RLock()
	defer h.mu.RUnlock()
	if len(h.sorted) == 0 {
		return ""
	}
	hk := hashString(key)
	idx := sort.Search(len(h.sorted), func(i int) bool { return h.sorted[i] >= hk })
	if idx == len(h.sorted) {
		idx = 0
	}
	return h.ring[h.sorted[idx]]
}

func hashKey(node string, replica int) uint32 {
	h := fnv.New32a()
	h.Write([]byte(node))
	h.Write([]byte{byte(replica >> 8), byte(replica)})
	return h.Sum32()
}

func hashString(s string) uint32 {
	h := fnv.New32a()
	h.Write([]byte(s))
	return h.Sum32()
}
