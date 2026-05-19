package ratelimiterv2

import (
	"sync"
	"time"
)

// Key identifies a rate-limit subject by user and endpoint.
type Key struct {
	UserID   string
	Endpoint string
}

// Config defines the rate limit parameters for a key.
type Config struct {
	Limit          int
	Window         time.Duration
	BurstAllowance int
}

// RateLimitInfo describes the current rate-limit state after an Allow call.
type RateLimitInfo struct {
	Remaining      int
	ResetAt        time.Time
	BurstRemaining int
}

// entry tracks request timestamps for one key.
type entry struct {
	cfg         Config
	timestamps  []time.Time // sliding window timestamps
	burstUsed   int
	windowStart time.Time
}

// Limiter is a thread-safe, per-key sliding-window rate limiter with burst support.
type Limiter struct {
	mu      sync.Mutex
	entries map[Key]*entry
}

// NewLimiter returns an initialised Limiter.
func NewLimiter() *Limiter {
	return &Limiter{entries: make(map[Key]*entry)}
}

// Configure sets the rate-limit configuration for a key.
func (l *Limiter) Configure(key Key, cfg Config) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries[key] = &entry{cfg: cfg, windowStart: time.Time{}}
}

// Allow checks whether a request at time now is permitted for the key.
// Returns (true, info) when allowed, (false, info) when rate-limited.
func (l *Limiter) Allow(key Key, now time.Time) (bool, RateLimitInfo) {
	l.mu.Lock()
	defer l.mu.Unlock()

	e, ok := l.entries[key]
	if !ok {
		// No configuration: deny.
		return false, RateLimitInfo{}
	}

	cfg := e.cfg

	// Evict timestamps outside the sliding window.
	cutoff := now.Add(-cfg.Window)
	valid := e.timestamps[:0]
	for _, ts := range e.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	e.timestamps = valid

	// Detect window cycle reset for burst tracking.
	// When the sliding window completely clears, reset burst counter.
	if len(e.timestamps) == 0 {
		e.burstUsed = 0
	}

	totalLimit := cfg.Limit + cfg.BurstAllowance
	count := len(e.timestamps)

	resetAt := now.Add(cfg.Window)
	if len(e.timestamps) > 0 {
		resetAt = e.timestamps[0].Add(cfg.Window)
	}

	if count >= totalLimit {
		burstRemaining := cfg.BurstAllowance - e.burstUsed
		if burstRemaining < 0 {
			burstRemaining = 0
		}
		return false, RateLimitInfo{
			Remaining:      0,
			ResetAt:        resetAt,
			BurstRemaining: burstRemaining,
		}
	}

	// Allow the request.
	e.timestamps = append(e.timestamps, now)

	// Track burst usage: requests beyond the base limit consume burst.
	if count+1 > cfg.Limit {
		e.burstUsed++
	}

	remaining := totalLimit - (count + 1)
	burstRemaining := cfg.BurstAllowance - e.burstUsed
	if burstRemaining < 0 {
		burstRemaining = 0
	}

	return true, RateLimitInfo{
		Remaining:      remaining,
		ResetAt:        resetAt,
		BurstRemaining: burstRemaining,
	}
}
