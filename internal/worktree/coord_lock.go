package worktree

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// CoordLockEntry is the on-disk JSON schema for a coordination lock.
type CoordLockEntry struct {
	AgentID    string    `json:"agent_id"`
	AcquiredAt time.Time `json:"acquired_at"`
	Branch     string    `json:"branch"`
	Repo       string    `json:"repo"`
}

// ErrLockHeld signals that a different agent already holds the lock.
type ErrLockHeld struct {
	Holder string
	Repo   string
	Branch string
}

func (e *ErrLockHeld) Error() string {
	return fmt.Sprintf(
		"worktree lock held by %q on %s/%s",
		e.Holder, e.Repo, e.Branch,
	)
}

// CoordLocker provides file-based mutual exclusion for multi-agent
// worktree coordination. Lock files live at
// <root>/.locks/<repo>/<sanitized-branch>.lock.
type CoordLocker struct {
	root string
	ttl  time.Duration
}

// NewCoordLocker creates a locker with lock files under root/.locks/.
func NewCoordLocker(root string, ttl time.Duration) *CoordLocker {
	if ttl <= 0 {
		ttl = 2 * time.Hour
	}
	return &CoordLocker{root: root, ttl: ttl}
}

// Acquire creates a lock file for the given repo+branch+agent.
// Re-entrant: the same agent can re-acquire its own lock.
// Expired locks (older than TTL) are auto-released.
func (cl *CoordLocker) Acquire(_ context.Context, repo, branch, agentID string) error {
	lockPath := cl.lockPath(repo, branch)
	if err := cl.checkExisting(lockPath, agentID, repo, branch); err != nil {
		return err
	}
	return cl.writeLock(lockPath, repo, branch, agentID)
}

func (cl *CoordLocker) checkExisting(lockPath, agentID, repo, branch string) error {
	existing, err := cl.readLock(lockPath)
	if err != nil {
		return err
	}
	if existing == nil || existing.AgentID == agentID {
		return nil
	}
	if time.Since(existing.AcquiredAt) >= cl.ttl {
		return nil
	}
	return &ErrLockHeld{Holder: existing.AgentID, Repo: repo, Branch: branch}
}

// Release removes the lock file if agentID matches the holder.
func (cl *CoordLocker) Release(repo, branch, agentID string) error {
	lockPath := cl.lockPath(repo, branch)
	existing, err := cl.readLock(lockPath)
	if err != nil {
		return err
	}
	if existing == nil {
		return nil
	}
	if existing.AgentID != agentID {
		return fmt.Errorf("worktree: cannot release lock held by %q (requester: %q)", existing.AgentID, agentID)
	}
	return os.Remove(lockPath)
}

// Check returns the current lock holder, or nil if unlocked / expired.
func (cl *CoordLocker) Check(repo, branch string) (*CoordLockEntry, error) {
	lockPath := cl.lockPath(repo, branch)
	entry, err := cl.readLock(lockPath)
	if err != nil {
		return nil, err
	}
	if entry == nil {
		return nil, nil
	}
	if time.Since(entry.AcquiredAt) >= cl.ttl {
		_ = os.Remove(lockPath)
		return nil, nil
	}
	return entry, nil
}

func (cl *CoordLocker) lockPath(repo, branch string) string {
	return filepath.Join(cl.root, ".locks", repo, sanitizeBranch(branch)+".lock")
}

func (cl *CoordLocker) readLock(path string) (*CoordLockEntry, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("worktree coord lock read: %w", err)
	}
	var entry CoordLockEntry
	if err := json.Unmarshal(data, &entry); err != nil {
		_ = os.Remove(path)
		return nil, nil
	}
	return &entry, nil
}

func (cl *CoordLocker) writeLock(path, repo, branch, agentID string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("worktree coord lock mkdir: %w", err)
	}
	entry := CoordLockEntry{
		AgentID:    agentID,
		AcquiredAt: time.Now(),
		Branch:     branch,
		Repo:       repo,
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("worktree coord lock marshal: %w", err)
	}
	return os.WriteFile(path, data, 0o644)
}
