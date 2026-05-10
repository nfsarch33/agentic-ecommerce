package worktree

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func writeLockFile(t *testing.T, dir, filename string, info LockInfo) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(info)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, filename), data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestRaceDetector_NoConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	rd := NewRaceDetector(root)
	err := rd.Check("ecommerce", "feat/new-feature", "agent-A", 99999)
	if err != nil {
		t.Fatalf("expected no conflict; got %v", err)
	}
}

func TestRaceDetector_ConflictDetected(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	repoDir := filepath.Join(root, "ecommerce")
	writeLockFile(t, repoDir, "feat-work.lock", LockInfo{
		AgentID:   "agent-A",
		StartedAt: time.Now(),
		Branch:    "feat/work",
		PID:       os.Getpid(), // current process = running
	})

	rd := NewRaceDetector(root)
	err := rd.Check("ecommerce", "feat/work", "agent-B", os.Getpid())

	var raceErr *ErrWorktreeRaceDetected
	if !asRaceError(err, &raceErr) {
		t.Fatalf("expected ErrWorktreeRaceDetected; got %T: %v", err, err)
	}
	if raceErr.HolderAgent != "agent-A" || raceErr.RequestingAgent != "agent-B" {
		t.Fatalf("wrong agents: holder=%q requesting=%q", raceErr.HolderAgent, raceErr.RequestingAgent)
	}
	if raceErr.Branch != "feat/work" {
		t.Fatalf("wrong branch: %q", raceErr.Branch)
	}
	if raceErr.Resolution == "" {
		t.Fatal("expected non-empty resolution suggestion")
	}
}

func TestRaceDetector_StaleLockCleanup(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	repoDir := filepath.Join(root, "ecommerce")
	writeLockFile(t, repoDir, "feat-stale.lock", LockInfo{
		AgentID:   "agent-dead",
		StartedAt: time.Now().Add(-3 * time.Hour),
		Branch:    "feat/stale",
		PID:       999999999, // PID that certainly doesn't exist
	})

	rd := NewRaceDetector(root)
	err := rd.Check("ecommerce", "feat/stale", "agent-B", os.Getpid())
	if err != nil {
		t.Fatalf("stale lock should be auto-cleaned; got %v", err)
	}

	lockPath := filepath.Join(repoDir, "feat-stale.lock")
	if _, err := os.Stat(lockPath); !os.IsNotExist(err) {
		t.Fatalf("stale lock file should have been removed")
	}
}

func TestRaceDetector_SameBranchCollision(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	repoDir := filepath.Join(root, "myrepo")
	writeLockFile(t, repoDir, "main.lock", LockInfo{
		AgentID:   "agent-1",
		StartedAt: time.Now(),
		Branch:    "main",
		PID:       os.Getpid(),
	})

	rd := NewRaceDetector(root)
	err := rd.Check("myrepo", "main", "agent-2", os.Getpid())

	var raceErr *ErrWorktreeRaceDetected
	if !asRaceError(err, &raceErr) {
		t.Fatalf("expected collision on same branch; got %T: %v", err, err)
	}
	if raceErr.Branch != "main" {
		t.Fatalf("expected branch=main; got %q", raceErr.Branch)
	}
}

func TestRaceDetector_DifferentBranchNoConflict(t *testing.T) {
	t.Parallel()
	root := t.TempDir()

	repoDir := filepath.Join(root, "ecommerce")
	writeLockFile(t, repoDir, "feat-alpha.lock", LockInfo{
		AgentID:   "agent-A",
		StartedAt: time.Now(),
		Branch:    "feat/alpha",
		PID:       os.Getpid(),
	})

	rd := NewRaceDetector(root)
	err := rd.Check("ecommerce", "feat/beta", "agent-B", os.Getpid())
	if err != nil {
		t.Fatalf("different branches should not conflict; got %v", err)
	}
}

func asRaceError(err error, target **ErrWorktreeRaceDetected) bool {
	if err == nil {
		return false
	}
	e, ok := err.(*ErrWorktreeRaceDetected)
	if ok {
		*target = e
	}
	return ok
}
