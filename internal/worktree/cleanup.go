package worktree

import (
	"context"
	"time"
)

// Staleness reasons used for Prometheus labels and reporting.
const (
	ReasonNoRecentCommit = "no_recent_commit"
	ReasonMergedIntoMain = "merged_into_main"
)

// WorktreeEntry is the input to the stale-scan algorithm. Callers
// populate these from `git worktree list` output + lock file checks +
// `git branch --merged main` inspection.
type WorktreeEntry struct {
	Repo       string
	Branch     string
	Path       string
	LastCommit time.Time
	HasLock    bool
	MergedMain bool
}

// StaleWorktree is a worktree that the scan determined is safe to remove.
type StaleWorktree struct {
	Entry  WorktreeEntry
	Reason string
}

// CleanupReport summarises a cleanup run.
type CleanupReport struct {
	Planned []StaleWorktree
	Deleted []string
	Errors  []string
	DryRun  bool
	RunAt   time.Time
}

// DeleteFunc is the callback that actually removes a worktree path.
// Callers provide the real implementation (git worktree remove + prune);
// tests inject a no-op or recorder.
type DeleteFunc func(ctx context.Context, path string) error

// ScanStale returns worktrees that are candidates for removal:
//   - No lock AND no recent commits (older than maxAge)
//   - Branch already merged into main (regardless of age)
//
// Active worktrees (locked or with recent commits within maxAge) are
// never returned.
func ScanStale(entries []WorktreeEntry, now time.Time, maxAge time.Duration) []StaleWorktree {
	var stale []StaleWorktree
	for _, e := range entries {
		if e.HasLock {
			continue
		}
		switch {
		case e.MergedMain:
			stale = append(stale, StaleWorktree{Entry: e, Reason: ReasonMergedIntoMain})
		case now.Sub(e.LastCommit) > maxAge:
			stale = append(stale, StaleWorktree{Entry: e, Reason: ReasonNoRecentCommit})
		}
	}
	return stale
}

// CleanupStale executes (or dry-runs) a cleanup of the given stale
// worktrees. When dryRun is true, the report is populated but deleteFn
// is never called.
func CleanupStale(
	ctx context.Context,
	worktrees []StaleWorktree,
	dryRun bool,
	deleteFn DeleteFunc,
	now time.Time,
) CleanupReport {
	report := CleanupReport{
		Planned: worktrees,
		DryRun:  dryRun,
		RunAt:   now,
	}

	if dryRun {
		return report
	}

	for _, sw := range worktrees {
		if err := deleteFn(ctx, sw.Entry.Path); err != nil {
			report.Errors = append(report.Errors, sw.Entry.Path+": "+err.Error())
			continue
		}
		report.Deleted = append(report.Deleted, sw.Entry.Path)
	}
	return report
}
