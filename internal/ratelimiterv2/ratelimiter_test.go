package ratelimiterv2

import (
	"testing"
	"time"
)

func TestLimiter_AllowWithinLimit(t *testing.T) {
	t.Parallel()

	l := NewLimiter()
	k := Key{UserID: "alice", Endpoint: "/api/v1/orders"}
	l.Configure(k, Config{Limit: 5, Window: time.Minute, BurstAllowance: 0})

	now := time.Now()
	for i := 0; i < 5; i++ {
		ok, info := l.Allow(k, now.Add(time.Duration(i)*time.Millisecond))
		if !ok {
			t.Errorf("request %d should be allowed", i+1)
		}
		_ = info
	}
}

func TestLimiter_BlockAtLimit(t *testing.T) {
	t.Parallel()

	l := NewLimiter()
	k := Key{UserID: "bob", Endpoint: "/api/v1/orders"}
	l.Configure(k, Config{Limit: 3, Window: time.Minute, BurstAllowance: 0})

	now := time.Now()
	for i := 0; i < 3; i++ {
		ok, _ := l.Allow(k, now.Add(time.Duration(i)*time.Millisecond))
		if !ok {
			t.Fatalf("request %d should be allowed", i+1)
		}
	}

	ok, info := l.Allow(k, now.Add(3*time.Millisecond))
	if ok {
		t.Error("4th request should be blocked")
	}
	if info.Remaining != 0 {
		t.Errorf("Remaining = %d, want 0", info.Remaining)
	}
}

func TestLimiter_BurstAllowance(t *testing.T) {
	t.Parallel()

	l := NewLimiter()
	k := Key{UserID: "carol", Endpoint: "/search"}
	l.Configure(k, Config{Limit: 3, Window: time.Minute, BurstAllowance: 2})

	now := time.Now()
	// Base 3 + burst 2 = 5 total allowed.
	for i := 0; i < 5; i++ {
		ok, _ := l.Allow(k, now.Add(time.Duration(i)*time.Millisecond))
		if !ok {
			t.Errorf("request %d should be allowed (burst)", i+1)
		}
	}

	ok, _ := l.Allow(k, now.Add(5*time.Millisecond))
	if ok {
		t.Error("6th request should be blocked even with burst")
	}
}

func TestLimiter_DifferentEndpointsIndependent(t *testing.T) {
	t.Parallel()

	l := NewLimiter()
	k1 := Key{UserID: "dave", Endpoint: "/ep1"}
	k2 := Key{UserID: "dave", Endpoint: "/ep2"}
	cfg := Config{Limit: 2, Window: time.Minute, BurstAllowance: 0}
	l.Configure(k1, cfg)
	l.Configure(k2, cfg)

	now := time.Now()
	// Exhaust k1.
	l.Allow(k1, now)
	l.Allow(k1, now.Add(time.Millisecond))

	// k2 should still be usable.
	ok, _ := l.Allow(k2, now.Add(2*time.Millisecond))
	if !ok {
		t.Error("different endpoint should be independent")
	}
}

func TestLimiter_WindowExpiry(t *testing.T) {
	t.Parallel()

	l := NewLimiter()
	k := Key{UserID: "eve", Endpoint: "/api"}
	l.Configure(k, Config{Limit: 2, Window: 100 * time.Millisecond, BurstAllowance: 0})

	now := time.Now()
	l.Allow(k, now)
	l.Allow(k, now.Add(10*time.Millisecond))

	// Both slots used; blocked now.
	ok, _ := l.Allow(k, now.Add(20*time.Millisecond))
	if ok {
		t.Error("should be blocked before window expiry")
	}

	// After window expires, should allow again.
	later := now.Add(200 * time.Millisecond)
	ok, _ = l.Allow(k, later)
	if !ok {
		t.Error("should be allowed after window expiry")
	}
}

func TestLimiter_UnconfiguredKey(t *testing.T) {
	t.Parallel()

	l := NewLimiter()
	k := Key{UserID: "ghost", Endpoint: "/nowhere"}
	ok, _ := l.Allow(k, time.Now())
	if ok {
		t.Error("unconfigured key should be denied")
	}
}
