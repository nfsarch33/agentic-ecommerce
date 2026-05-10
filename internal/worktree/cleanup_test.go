package worktree

import (
	"context"
	"testing"
	"time"
)

func TestScanStale_DetectsStale(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	maxAge := 48 * time.Hour

	entries := []WorktreeEntry{
		{
			Repo:       "ecommerce",
			Branch:     "feat/old",
			Path:       "/wt/ecommerce/feat-old",
			LastCommit: now.Add(-72 * time.Hour),
			HasLock:    false,
			MergedMain: false,
		},
	}

	stale := ScanStale(entries, now, maxAge)
	if len(stale) != 1 {
		t.Fatalf("expected 1 stale; got %d", len(stale))
	}
	if stale[0].Reason != ReasonNoRecentCommit {
		t.Fatalf("expected reason=%q; got %q", ReasonNoRecentCommit, stale[0].Reason)
	}
}

func TestScanStale_ActiveNotTouched(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	maxAge := 48 * time.Hour

	entries := []WorktreeEntry{
		{
			Repo:       "ecommerce",
			Branch:     "feat/active",
			Path:       "/wt/ecommerce/feat-active",
			LastCommit: now.Add(-12 * time.Hour),
			HasLock:    true,
			MergedMain: false,
		},
	}

	stale := ScanStale(entries, now, maxAge)
	if len(stale) != 0 {
		t.Fatalf("active worktree should not be stale; got %d entries", len(stale))
	}
}

func TestScanStale_MergedBranchCleaned(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	maxAge := 48 * time.Hour

	entries := []WorktreeEntry{
		{
			Repo:       "ecommerce",
			Branch:     "feat/merged",
			Path:       "/wt/ecommerce/feat-merged",
			LastCommit: now.Add(-1 * time.Hour),
			HasLock:    false,
			MergedMain: true,
		},
	}

	stale := ScanStale(entries, now, maxAge)
	if len(stale) != 1 {
		t.Fatalf("merged branch should be stale; got %d", len(stale))
	}
	if stale[0].Reason != ReasonMergedIntoMain {
		t.Fatalf("expected reason=%q; got %q", ReasonMergedIntoMain, stale[0].Reason)
	}
}

func TestCleanupStale_DryRunReportsWithoutDeleting(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)

	staleList := []StaleWorktree{
		{
			Entry:  WorktreeEntry{Repo: "ecommerce", Branch: "feat/old", Path: "/wt/old"},
			Reason: ReasonNoRecentCommit,
		},
		{
			Entry:  WorktreeEntry{Repo: "ecommerce", Branch: "feat/merged", Path: "/wt/merged"},
			Reason: ReasonMergedIntoMain,
		},
	}

	var deleted []string
	recorder := func(_ context.Context, path string) error {
		deleted = append(deleted, path)
		return nil
	}

	report := CleanupStale(context.Background(), staleList, true, recorder, now)

	if len(report.Planned) != 2 {
		t.Fatalf("expected 2 planned; got %d", len(report.Planned))
	}
	if len(deleted) != 0 {
		t.Fatalf("dry-run must not delete; got %v", deleted)
	}
	if report.DryRun != true {
		t.Fatal("report must indicate dry-run")
	}
}

func TestCleanupStale_ActualRunDeletes(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)

	staleList := []StaleWorktree{
		{
			Entry:  WorktreeEntry{Repo: "ecommerce", Branch: "feat/old", Path: "/wt/old"},
			Reason: ReasonNoRecentCommit,
		},
	}

	var deleted []string
	recorder := func(_ context.Context, path string) error {
		deleted = append(deleted, path)
		return nil
	}

	report := CleanupStale(context.Background(), staleList, false, recorder, now)

	if len(deleted) != 1 || deleted[0] != "/wt/old" {
		t.Fatalf("expected delete of /wt/old; got %v", deleted)
	}
	if report.DryRun {
		t.Fatal("report must not indicate dry-run")
	}
}
