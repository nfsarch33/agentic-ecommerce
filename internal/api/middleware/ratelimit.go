package middleware

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"
)

// bucket tracks the sliding-window state for one client.
type bucket struct {
	mu         sync.Mutex
	timestamps []time.Time
}

// RateLimiter implements a per-IP sliding window rate limiter.
type RateLimiter struct {
	limit  int
	window time.Duration

	mu      sync.Mutex
	buckets map[string]*bucket
}

func NewRateLimiter(requests int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		limit:   requests,
		window:  window,
		buckets: make(map[string]*bucket),
	}
}

// Middleware returns an http.Handler that enforces the rate limit.
func (rl *RateLimiter) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !rl.allow(ip) {
			w.Header().Set("Retry-After", fmt.Sprintf("%d", int(rl.window.Seconds())))
			http.Error(w, "rate limit exceeded", http.StatusTooManyRequests)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (rl *RateLimiter) allow(ip string) bool {
	rl.mu.Lock()
	b, ok := rl.buckets[ip]
	if !ok {
		b = &bucket{}
		rl.buckets[ip] = b
	}
	rl.mu.Unlock()

	b.mu.Lock()
	defer b.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-rl.window)

	// evict timestamps outside the window
	valid := b.timestamps[:0]
	for _, ts := range b.timestamps {
		if ts.After(cutoff) {
			valid = append(valid, ts)
		}
	}
	b.timestamps = valid

	if len(b.timestamps) >= rl.limit {
		return false
	}
	b.timestamps = append(b.timestamps, now)
	return true
}

func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}
