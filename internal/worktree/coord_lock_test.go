package worktree

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestCoordLock_AcquireAndRelease(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cl := NewCoordLocker(root, 2*time.Hour)

	ctx := context.Background()
	err := cl.Acquire(ctx, "ecommerce", "feat/new", "agent-1")
	if err != nil {
		t.Fatalf("acquire failed: %v", err)
	}

	holder, err := cl.Check("ecommerce", "feat/new")
	if err != nil {
		t.Fatalf("check failed: %v", err)
	}
	if holder == nil || holder.AgentID != "agent-1" {
		t.Fatalf("expected holder=agent-1; got %+v", holder)
	}

	err = cl.Release("ecommerce", "feat/new", "agent-1")
	if err != nil {
		t.Fatalf("release failed: %v", err)
	}

	holder, err = cl.Check("ecommerce", "feat/new")
	if err != nil {
		t.Fatalf("check after release failed: %v", err)
	}
	if holder != nil {
		t.Fatalf("expected nil holder after release; got %+v", holder)
	}
}

func TestCoordLock_DoubleAcquireSameAgentOK(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cl := NewCoordLocker(root, 2*time.Hour)

	ctx := context.Background()
	if err := cl.Acquire(ctx, "runx", "main", "agent-A"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}
	if err := cl.Acquire(ctx, "runx", "main", "agent-A"); err != nil {
		t.Fatalf("re-entrant acquire by same agent must succeed: %v", err)
	}
}

func TestCoordLock_DoubleAcquireDifferentAgentBlocked(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cl := NewCoordLocker(root, 2*time.Hour)

	ctx := context.Background()
	if err := cl.Acquire(ctx, "ecommerce", "feat/x", "agent-1"); err != nil {
		t.Fatalf("first acquire: %v", err)
	}

	err := cl.Acquire(ctx, "ecommerce", "feat/x", "agent-2")
	if err == nil {
		t.Fatal("second agent must be blocked")
	}

	e, ok := err.(*ErrLockHeld)
	if !ok {
		t.Fatalf("expected *ErrLockHeld; got %T: %v", err, err)
	}
	if e.Holder != "agent-1" {
		t.Fatalf("expected holder=agent-1; got %q", e.Holder)
	}
}

func TestCoordLock_TTLExpiry(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cl := NewCoordLocker(root, 100*time.Millisecond)

	ctx := context.Background()
	if err := cl.Acquire(ctx, "ecommerce", "feat/ttl", "agent-old"); err != nil {
		t.Fatalf("acquire: %v", err)
	}

	time.Sleep(150 * time.Millisecond)

	if err := cl.Acquire(ctx, "ecommerce", "feat/ttl", "agent-new"); err != nil {
		t.Fatalf("expired lock should allow new acquire: %v", err)
	}

	holder, _ := cl.Check("ecommerce", "feat/ttl")
	if holder == nil || holder.AgentID != "agent-new" {
		t.Fatalf("expected holder=agent-new; got %+v", holder)
	}
}

func TestCoordLock_CheckUnlockedReturnsNil(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cl := NewCoordLocker(root, 2*time.Hour)

	holder, err := cl.Check("nonexistent", "any-branch")
	if err != nil {
		t.Fatalf("check on unlocked: %v", err)
	}
	if holder != nil {
		t.Fatalf("expected nil; got %+v", holder)
	}
}

func TestCoordLock_LockFileLocation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	cl := NewCoordLocker(root, 2*time.Hour)

	ctx := context.Background()
	_ = cl.Acquire(ctx, "myrepo", "feat/slash-branch", "agent-1")

	expected := filepath.Join(root, ".locks", "myrepo", "feat-slash-branch.lock")
	if _, err := os.Stat(expected); err != nil {
		t.Fatalf("lock file not at expected path %q: %v", expected, err)
	}
}
