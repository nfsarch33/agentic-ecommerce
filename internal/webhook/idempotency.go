package webhook

import (
	"context"
	"sync"
)

// IdempotencyStore guards duplicate webhook deliveries. v3.3.0
// ships the in-memory store; the v3.7.0 Postgres-backed store
// drops in via the same interface.
type IdempotencyStore interface {
	// Reserve returns true when the (tenantID, key) pair has not
	// been seen before; false on duplicate. The store records the
	// pair on first call so the next Reserve returns false.
	Reserve(ctx context.Context, tenantID, key string) (bool, error)
}

// MemoryIdempotencyStore is the in-memory implementation; safe for
// concurrent use by multiple handler goroutines.
type MemoryIdempotencyStore struct {
	mu   sync.Mutex
	seen map[string]struct{}
}

// NewMemoryIdempotencyStore returns an empty store.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{seen: map[string]struct{}{}}
}

// Reserve atomically records the pair. Returns (true, nil) on
// first observation and (false, nil) on subsequent calls.
func (s *MemoryIdempotencyStore) Reserve(_ context.Context, tenantID, key string) (bool, error) {
	if tenantID == "" || key == "" {
		return false, ErrWebhookPayloadInvalid
	}
	composite := tenantID + "\x00" + key
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[composite]; ok {
		return false, nil
	}
	s.seen[composite] = struct{}{}
	return true, nil
}
