package cache_test

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/cache"
)

func TestCache_SetAndGet(t *testing.T) {
	t.Parallel()
	c := cache.NewDistributedCache()
	ctx := context.Background()
	if err := c.Set(ctx, "key1", []byte("value1"), 0); err != nil {
		t.Fatalf("set failed: %v", err)
	}
	val, err := c.Get(ctx, "key1")
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	if string(val) != "value1" {
		t.Fatalf("expected value1, got %s", val)
	}
}

func TestCache_GetMissReturnsError(t *testing.T) {
	t.Parallel()
	c := cache.NewDistributedCache()
	_, err := c.Get(context.Background(), "nonexistent")
	if err != cache.ErrCacheMiss {
		t.Fatalf("expected ErrCacheMiss, got %v", err)
	}
}

func TestCache_TTLExpiry(t *testing.T) {
	t.Parallel()
	c := cache.NewDistributedCache()
	ctx := context.Background()
	c.Set(ctx, "ttlkey", []byte("data"), 10*time.Millisecond)
	time.Sleep(20 * time.Millisecond)
	_, err := c.Get(ctx, "ttlkey")
	if err != cache.ErrCacheMiss {
		t.Fatalf("expected cache miss after TTL expiry, got %v", err)
	}
}

func TestCache_InvalidateRemovesKey(t *testing.T) {
	t.Parallel()
	c := cache.NewDistributedCache()
	ctx := context.Background()
	c.Set(ctx, "k", []byte("v"), 0)
	c.Invalidate(ctx, "k")
	_, err := c.Get(ctx, "k")
	if err != cache.ErrCacheMiss {
		t.Fatal("expected key to be invalidated")
	}
}

func TestCache_InvalidatePatternRemovesPrefix(t *testing.T) {
	t.Parallel()
	c := cache.NewDistributedCache()
	ctx := context.Background()
	c.Set(ctx, "user:1", []byte("a"), 0)
	c.Set(ctx, "user:2", []byte("b"), 0)
	c.Set(ctx, "order:1", []byte("c"), 0)
	c.InvalidatePattern(ctx, "user:*")
	if _, err := c.Get(ctx, "user:1"); err != cache.ErrCacheMiss {
		t.Fatal("expected user:1 invalidated")
	}
	if _, err := c.Get(ctx, "order:1"); err != nil {
		t.Fatal("expected order:1 still present")
	}
}

func TestCache_ConsistentHashDistributes(t *testing.T) {
	t.Parallel()
	h := cache.NewConsistentHash(100)
	h.AddNode("node1")
	h.AddNode("node2")
	h.AddNode("node3")
	counts := make(map[string]int)
	for i := 0; i < 1000; i++ {
		key := string(rune('a'+i%26)) + string(rune('A'+i%26))
		node := h.GetNode(key + string(rune(i)))
		counts[node]++
	}
	if len(counts) < 2 {
		t.Fatalf("expected distribution across nodes, got %v", counts)
	}
}

func TestCache_ConcurrentSetGetSafe(t *testing.T) {
	t.Parallel()
	c := cache.NewDistributedCache()
	ctx := context.Background()
	done := make(chan struct{})
	for i := 0; i < 20; i++ {
		go func(i int) {
			key := string(rune('a' + i%26))
			c.Set(ctx, key, []byte("v"), 0)
			c.Get(ctx, key)
			done <- struct{}{}
		}(i)
	}
	for i := 0; i < 20; i++ {
		<-done
	}
}
