package marketplace

import (
	"errors"
	"testing"
	"time"
)

func TestSandboxBudget(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	sb := NewSandbox(SandboxConfig{HookBudget: 3, Window: time.Minute, HookTimeout: time.Second, Now: clock})
	for i := 0; i < 3; i++ {
		if err := sb.RecordHook("tenant-a", "stripe", "activate"); err != nil {
			t.Fatalf("hook %d: %v", i, err)
		}
	}
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); !errors.Is(err, ErrSandboxBudgetExceeded) {
		t.Fatalf("expected ErrSandboxBudgetExceeded after 3 hooks, got %v", err)
	}
}

func TestSandboxRefill(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	sb := NewSandbox(SandboxConfig{HookBudget: 1, Window: time.Minute, HookTimeout: time.Second, Now: clock})
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); err != nil {
		t.Fatalf("hook 1: %v", err)
	}
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); !errors.Is(err, ErrSandboxBudgetExceeded) {
		t.Fatalf("expected budget exhaustion, got %v", err)
	}
	now = now.Add(2 * time.Minute)
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); err != nil {
		t.Fatalf("post-refill hook: %v", err)
	}
}

func TestSandboxIsolatesTenants(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	sb := NewSandbox(SandboxConfig{HookBudget: 1, Window: time.Minute, HookTimeout: time.Second, Now: clock})
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); err != nil {
		t.Fatalf("tenant A first hook: %v", err)
	}
	// Tenant B has its own bucket and is not affected by A's exhaustion.
	if err := sb.RecordHook("tenant-b", "stripe", "activate"); err != nil {
		t.Fatalf("tenant B should still have budget: %v", err)
	}
	// Tenant A is now exhausted.
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); !errors.Is(err, ErrSandboxBudgetExceeded) {
		t.Fatalf("tenant A should be exhausted, got %v", err)
	}
}

func TestSandboxIsolatesPlugins(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	sb := NewSandbox(SandboxConfig{HookBudget: 1, Window: time.Minute, HookTimeout: time.Second, Now: clock})
	if err := sb.RecordHook("tenant-a", "stripe", "activate"); err != nil {
		t.Fatalf("plugin A first hook: %v", err)
	}
	// Different plugin within the same tenant has its own bucket.
	if err := sb.RecordHook("tenant-a", "ses-email", "activate"); err != nil {
		t.Fatalf("plugin B should still have budget: %v", err)
	}
}

func TestSandboxHookTimeout(t *testing.T) {
	t.Parallel()
	sb := NewSandbox(SandboxConfig{HookTimeout: 7 * time.Second})
	if got := sb.HookTimeout(); got != 7*time.Second {
		t.Fatalf("HookTimeout = %v, want 7s", got)
	}
	def := NewSandbox(SandboxConfig{})
	if got := def.HookTimeout(); got != 30*time.Second {
		t.Fatalf("default HookTimeout = %v, want 30s", got)
	}
}

func TestPermissiveSandboxNeverExhausts(t *testing.T) {
	t.Parallel()
	sb := NewPermissiveSandbox()
	for i := 0; i < 1000; i++ {
		if err := sb.RecordHook("tenant", "slug", "activate"); err != nil {
			t.Fatalf("permissive sandbox should never exhaust: %v", err)
		}
	}
}

func TestSandboxHooksRecordedCounter(t *testing.T) {
	t.Parallel()
	sb := NewPermissiveSandbox()
	for i := 0; i < 5; i++ {
		_ = sb.RecordHook("tenant", "slug", "activate")
	}
	if got := sb.HooksRecorded(); got != 5 {
		t.Fatalf("HooksRecorded = %d, want 5", got)
	}
}
