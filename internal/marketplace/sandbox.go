package marketplace

import (
	"fmt"
	"sync"
	"time"
)

// SandboxConfig tunes the sandbox limits. Zero values fall back to
// production-friendly defaults so the simple `NewSandbox(SandboxConfig{})`
// call always works.
type SandboxConfig struct {
	// HookBudget bounds the lifecycle-hook invocations per tenant per
	// slug per Window. v2.4.0 default is 60 invocations per minute,
	// which is generous for legitimate plugins and tight enough that
	// a runaway loop trips ErrSandboxBudgetExceeded fast.
	HookBudget int
	// Window is the rate-limit window. Default: 1 minute.
	Window time.Duration
	// HookTimeout caps a single Install/Activate/Deactivate/Uninstall
	// hook invocation. Default: 30 seconds, per the spec.
	HookTimeout time.Duration
	// Now is the clock source. Tests inject a fake; production uses
	// time.Now.
	Now func() time.Time
}

// Sandbox enforces per-(tenant, slug) hook rate limits using an
// in-memory token bucket. The bucket pattern mirrors
// internal/security/ratelimit.go so the cognitive overhead stays low
// for reviewers familiar with that file.
//
// The Sandbox is intentionally narrow in v2.4.0: tenant scoping is
// enforced by always keying the bucket on (tenantID, slug), and hook
// timeout is exposed as HookTimeout for callers that want to wrap
// their hook calls in context.WithTimeout. v2.5.0+ may extend with
// outbound HTTP rate limiting once plugin SDK ships.
type Sandbox struct {
	mu         sync.Mutex
	cfg        SandboxConfig
	buckets    map[string]bucket
	hooksTotal uint64
}

type bucket struct {
	tokens    int
	updatedAt time.Time
}

// NewSandbox returns a configured sandbox.
func NewSandbox(cfg SandboxConfig) *Sandbox {
	if cfg.HookBudget <= 0 {
		cfg.HookBudget = 60
	}
	if cfg.Window <= 0 {
		cfg.Window = time.Minute
	}
	if cfg.HookTimeout <= 0 {
		cfg.HookTimeout = 30 * time.Second
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &Sandbox{cfg: cfg, buckets: make(map[string]bucket)}
}

// NewPermissiveSandbox returns a sandbox with very high budgets,
// suitable for tests that don't care about rate limiting.
func NewPermissiveSandbox() *Sandbox {
	return NewSandbox(SandboxConfig{
		HookBudget:  1_000_000,
		Window:      time.Second,
		HookTimeout: time.Minute,
	})
}

// HookTimeout returns the configured per-hook timeout so callers can
// wrap their context.
func (s *Sandbox) HookTimeout() time.Duration { return s.cfg.HookTimeout }

// RecordHook decrements the per-(tenant, slug) token bucket. Returns
// ErrSandboxBudgetExceeded when the bucket is empty.
func (s *Sandbox) RecordHook(tenantID, slug, action string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := tenantID + "::" + slug
	now := s.cfg.Now()
	state, ok := s.buckets[key]
	if !ok {
		state = bucket{tokens: s.cfg.HookBudget, updatedAt: now}
	}
	state = refill(state, s.cfg.HookBudget, s.cfg.Window, now)
	if state.tokens <= 0 {
		s.buckets[key] = state
		return fmt.Errorf("%w: tenant=%s slug=%s action=%s", ErrSandboxBudgetExceeded, tenantID, slug, action)
	}
	state.tokens--
	s.buckets[key] = state
	s.hooksTotal++
	return nil
}

// HooksRecorded returns the global count for telemetry/tests.
func (s *Sandbox) HooksRecorded() uint64 {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.hooksTotal
}

// refill applies elapsed-window refills to a bucket without iterating
// over arbitrarily large gaps. Same algorithm as security.InMemoryTokenBucket.
func refill(state bucket, capacity int, window time.Duration, now time.Time) bucket {
	if elapsed := now.Sub(state.updatedAt); elapsed >= window {
		refills := int(elapsed / window)
		state.tokens += refills
		if state.tokens > capacity {
			state.tokens = capacity
		}
		state.updatedAt = state.updatedAt.Add(time.Duration(refills) * window)
	}
	return state
}
