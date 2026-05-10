//go:build v4151_smoke

package v4151_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/worktree"
)

// TestE2E_TwoAgentsDifferentBranches verifies that two agents can
// work on different branches simultaneously without interference.
func TestE2E_TwoAgentsDifferentBranches(t *testing.T) {
	root := t.TempDir()
	cl := worktree.NewCoordLocker(root, 2*time.Hour)
	rd := worktree.NewRaceDetector(filepath.Join(root, ".locks"))
	ctx := context.Background()

	if err := cl.Acquire(ctx, "ecommerce", "feat/agent-a-work", "agent-A"); err != nil {
		t.Fatalf("agent-A acquire failed: %v", err)
	}
	if err := cl.Acquire(ctx, "ecommerce", "feat/agent-b-work", "agent-B"); err != nil {
		t.Fatalf("agent-B acquire on different branch failed: %v", err)
	}

	if err := rd.Check("ecommerce", "feat/agent-a-work", "agent-A", os.Getpid()); err != nil {
		t.Fatalf("race check for agent-A should pass: %v", err)
	}
	if err := rd.Check("ecommerce", "feat/agent-b-work", "agent-B", os.Getpid()); err != nil {
		t.Fatalf("race check for agent-B should pass: %v", err)
	}

	_ = cl.Release("ecommerce", "feat/agent-a-work", "agent-A")
	_ = cl.Release("ecommerce", "feat/agent-b-work", "agent-B")
}

// TestE2E_TwoAgentsSameBranch verifies that the second agent is
// blocked when attempting the same branch as the first agent.
func TestE2E_TwoAgentsSameBranch(t *testing.T) {
	root := t.TempDir()
	cl := worktree.NewCoordLocker(root, 2*time.Hour)
	ctx := context.Background()

	if err := cl.Acquire(ctx, "ecommerce", "feat/shared", "agent-A"); err != nil {
		t.Fatalf("agent-A acquire: %v", err)
	}

	err := cl.Acquire(ctx, "ecommerce", "feat/shared", "agent-B")
	if err == nil {
		t.Fatal("agent-B must be blocked on same branch")
	}

	held, ok := err.(*worktree.ErrLockHeld)
	if !ok {
		t.Fatalf("expected *ErrLockHeld; got %T: %v", err, err)
	}
	if held.Holder != "agent-A" {
		t.Fatalf("expected holder=agent-A; got %q", held.Holder)
	}

	_ = cl.Release("ecommerce", "feat/shared", "agent-A")
}

// TestE2E_SequentialHandoff verifies that after the first agent
// finishes and releases, the second agent can acquire the lock.
func TestE2E_SequentialHandoff(t *testing.T) {
	root := t.TempDir()
	cl := worktree.NewCoordLocker(root, 2*time.Hour)
	ctx := context.Background()

	if err := cl.Acquire(ctx, "ecommerce", "feat/handoff", "agent-A"); err != nil {
		t.Fatalf("agent-A acquire: %v", err)
	}

	err := cl.Acquire(ctx, "ecommerce", "feat/handoff", "agent-B")
	if err == nil {
		t.Fatal("agent-B should be blocked while agent-A holds lock")
	}

	if err := cl.Release("ecommerce", "feat/handoff", "agent-A"); err != nil {
		t.Fatalf("agent-A release: %v", err)
	}

	if err := cl.Acquire(ctx, "ecommerce", "feat/handoff", "agent-B"); err != nil {
		t.Fatalf("agent-B acquire after release should succeed: %v", err)
	}

	holder, err := cl.Check("ecommerce", "feat/handoff")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if holder == nil || holder.AgentID != "agent-B" {
		t.Fatalf("expected holder=agent-B; got %+v", holder)
	}

	_ = cl.Release("ecommerce", "feat/handoff", "agent-B")
}

// TestE2E_CrashRecoveryTTL simulates an agent crash by using a very
// short TTL. After TTL expires, the stale lock is auto-released and
// a new agent can acquire.
func TestE2E_CrashRecoveryTTL(t *testing.T) {
	root := t.TempDir()
	ttl := 100 * time.Millisecond
	cl := worktree.NewCoordLocker(root, ttl)
	ctx := context.Background()

	if err := cl.Acquire(ctx, "ecommerce", "feat/crashed", "agent-crashed"); err != nil {
		t.Fatalf("crashed agent acquire: %v", err)
	}

	err := cl.Acquire(ctx, "ecommerce", "feat/crashed", "agent-recovery")
	if err == nil {
		t.Fatal("should be blocked before TTL")
	}

	time.Sleep(150 * time.Millisecond)

	if err := cl.Acquire(ctx, "ecommerce", "feat/crashed", "agent-recovery"); err != nil {
		t.Fatalf("recovery agent should acquire after TTL: %v", err)
	}

	holder, _ := cl.Check("ecommerce", "feat/crashed")
	if holder == nil || holder.AgentID != "agent-recovery" {
		t.Fatalf("expected holder=agent-recovery; got %+v", holder)
	}

	_ = cl.Release("ecommerce", "feat/crashed", "agent-recovery")
}
