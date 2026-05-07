package security

import (
	"context"
	"sync"
	"time"
)

type RateLimitDecision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type RateLimiter interface {
	Allow(ctx context.Context, key string) (RateLimitDecision, error)
}

type TokenBucketConfig struct {
	Capacity       int
	RefillInterval time.Duration
	Now            func() time.Time
}

type InMemoryTokenBucket struct {
	mu             sync.Mutex
	capacity       int
	refillInterval time.Duration
	now            func() time.Time
	buckets        map[string]bucketState
}

type bucketState struct {
	Tokens    int
	UpdatedAt time.Time
}

func NewInMemoryTokenBucket(cfg TokenBucketConfig) *InMemoryTokenBucket {
	capacity := cfg.Capacity
	if capacity <= 0 {
		capacity = 60
	}
	refillInterval := cfg.RefillInterval
	if refillInterval <= 0 {
		refillInterval = time.Minute
	}
	now := cfg.Now
	if now == nil {
		now = time.Now
	}
	return &InMemoryTokenBucket{
		capacity:       capacity,
		refillInterval: refillInterval,
		now:            now,
		buckets:        make(map[string]bucketState),
	}
}

func (l *InMemoryTokenBucket) Allow(_ context.Context, key string) (RateLimitDecision, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if key == "" {
		key = "anonymous"
	}
	now := l.now().UTC()
	state, ok := l.buckets[key]
	if !ok {
		state = bucketState{Tokens: l.capacity, UpdatedAt: now}
	}
	if elapsed := now.Sub(state.UpdatedAt); elapsed >= l.refillInterval {
		refills := int(elapsed / l.refillInterval)
		state.Tokens += refills
		if state.Tokens > l.capacity {
			state.Tokens = l.capacity
		}
		state.UpdatedAt = state.UpdatedAt.Add(time.Duration(refills) * l.refillInterval)
	}
	if state.Tokens <= 0 {
		retryAfter := l.refillInterval - now.Sub(state.UpdatedAt)
		if retryAfter <= 0 {
			retryAfter = l.refillInterval
		}
		l.buckets[key] = state
		return RateLimitDecision{Allowed: false, RetryAfter: retryAfter}, nil
	}
	state.Tokens--
	l.buckets[key] = state
	return RateLimitDecision{Allowed: true}, nil
}
