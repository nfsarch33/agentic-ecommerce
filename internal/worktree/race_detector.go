package worktree

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

// LockInfo is the JSON schema for worktree lock files stored at
// ~/runs/worktrees/<repo>/<sanitized-branch>.lock.
type LockInfo struct {
	AgentID   string    `json:"agent_id"`
	StartedAt time.Time `json:"started_at"`
	Branch    string    `json:"branch"`
	PID       int       `json:"pid"`
}

// ErrWorktreeRaceDetected signals that another agent already holds
// a lock on the requested branch within the same repo.
type ErrWorktreeRaceDetected struct {
	HolderAgent     string
	RequestingAgent string
	Branch          string
	Resolution      string
}

func (e *ErrWorktreeRaceDetected) Error() string {
	return fmt.Sprintf(
		"worktree race: agent %q holds branch %q (requested by %q); %s",
		e.HolderAgent, e.Branch, e.RequestingAgent, e.Resolution,
	)
}

// RaceDetector checks for concurrent modifications to worktrees
// by scanning lock files under the worktrees root directory.
type RaceDetector struct {
	root string
}

// NewRaceDetector returns a detector that scans lock files under root.
// root is typically ~/runs/worktrees.
func NewRaceDetector(root string) *RaceDetector {
	return &RaceDetector{root: root}
}

// Check verifies that no other agent holds a lock on the given
// repo+branch combination. Stale locks (PID no longer running) are
// auto-cleaned with a warning log.
func (rd *RaceDetector) Check(repo, branch, agentID string, _ int) error {
	lockPath, err := rd.findLockFile(repo, branch)
	if err != nil || lockPath == "" {
		return err
	}
	return rd.handleExistingLock(lockPath, branch, agentID)
}

func (rd *RaceDetector) findLockFile(repo, branch string) (string, error) {
	lockPath := filepath.Join(rd.root, repo, sanitizeBranch(branch)+".lock")
	_, err := os.Stat(lockPath)
	if os.IsNotExist(err) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("worktree race check: %w", err)
	}
	return lockPath, nil
}

func (rd *RaceDetector) handleExistingLock(path, branch, requestingAgent string) error {
	info, err := readLockInfo(path)
	if err != nil {
		return err
	}
	if info == nil || !processRunning(info.PID) {
		_ = os.Remove(path)
		return nil
	}
	if info.AgentID == requestingAgent {
		return nil
	}
	return newRaceError(info.AgentID, requestingAgent, branch)
}

func readLockInfo(path string) (*LockInfo, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("worktree race check read lock: %w", err)
	}
	var info LockInfo
	if err := json.Unmarshal(data, &info); err != nil {
		_ = os.Remove(path)
		return nil, nil
	}
	return &info, nil
}

func newRaceError(holder, requester, branch string) *ErrWorktreeRaceDetected {
	return &ErrWorktreeRaceDetected{
		HolderAgent:     holder,
		RequestingAgent: requester,
		Branch:          branch,
		Resolution:      fmt.Sprintf("wait for agent %q to finish, or use a different branch", holder),
	}
}

func processRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

func sanitizeBranch(branch string) string {
	r := strings.NewReplacer("/", "-", "\\", "-", ":", "-", "~", "-", " ", "-", "..", "-")
	out := r.Replace(strings.TrimSpace(branch))
	return strings.Trim(out, "-")
}
