// File scope: v6.1.0 coverage backfill -- the in-memory
// MarketplaceSubmissions adapter shipped without unit tests in v2.7.0.
// This file pins Create, Get, ListPending, and SaveState contracts.
package inmemory_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

func makeSubmission(id, tenant string, state marketplace.SubmissionState, submittedAt string) marketplace.Submission {
	return marketplace.Submission{
		ID:             id,
		TenantID:       tenant,
		SubmitterEmail: id + "@vendor.test",
		Manifest:       marketplace.Manifest{Name: id, Version: "1.0.0"},
		State:          state,
		SubmittedAt:    submittedAt,
	}
}

func TestMarketplaceSubmissionsCreateAndGet(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	row := makeSubmission("s-1", "tenant-a", marketplace.SubmissionPendingReview, "2026-05-11T10:00:00Z")
	if err := repo.Create(context.Background(), row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(context.Background(), "s-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID != row.ID || got.TenantID != row.TenantID {
		t.Fatalf("Get returned %+v, want %+v", got, row)
	}
}

func TestMarketplaceSubmissionsCreateDuplicateRejected(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	row := makeSubmission("s-dup", "tenant-a", marketplace.SubmissionPendingReview, "2026-05-11T10:00:00Z")
	if err := repo.Create(context.Background(), row); err != nil {
		t.Fatalf("Create seed: %v", err)
	}
	if err := repo.Create(context.Background(), row); !errors.Is(err, marketplace.ErrSubmissionAlreadyExists) {
		t.Fatalf("Create dup: err = %v, want ErrSubmissionAlreadyExists", err)
	}
}

func TestMarketplaceSubmissionsGetMissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	_, err := repo.Get(context.Background(), "missing")
	if !errors.Is(err, marketplace.ErrSubmissionNotFound) {
		t.Fatalf("Get missing: err = %v, want ErrSubmissionNotFound", err)
	}
}

func TestMarketplaceSubmissionsListPendingFiltersByStateAndTenant(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	ctx := context.Background()
	rows := []marketplace.Submission{
		makeSubmission("s-1", "tenant-a", marketplace.SubmissionPendingReview, "2026-05-11T09:00:00Z"),
		makeSubmission("s-2", "tenant-a", marketplace.SubmissionPendingReview, "2026-05-11T10:00:00Z"),
		makeSubmission("s-3", "tenant-b", marketplace.SubmissionPendingReview, "2026-05-11T11:00:00Z"),
		makeSubmission("s-4", "tenant-a", marketplace.SubmissionApproved, "2026-05-11T12:00:00Z"),
	}
	for _, r := range rows {
		if err := repo.Create(ctx, r); err != nil {
			t.Fatalf("Create %s: %v", r.ID, err)
		}
	}

	pageA, totalA, err := repo.ListPending(ctx, "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("ListPending tenant-a: %v", err)
	}
	if totalA != 2 {
		t.Fatalf("tenant-a total = %d, want 2", totalA)
	}
	if pageA[0].ID != "s-1" || pageA[1].ID != "s-2" {
		t.Fatalf("tenant-a order = %s,%s; want s-1,s-2", pageA[0].ID, pageA[1].ID)
	}

	_, totalCross, err := repo.ListPending(ctx, "", 1, 10)
	if err != nil {
		t.Fatalf("ListPending cross-tenant: %v", err)
	}
	if totalCross != 3 {
		t.Fatalf("cross-tenant pending total = %d, want 3", totalCross)
	}
}

func TestMarketplaceSubmissionsListPendingPaginationBoundaries(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		ts := "2026-05-11T0" + string(rune('0'+i)) + ":00:00Z"
		if err := repo.Create(ctx, makeSubmission("s-"+string(rune('0'+i)), "tenant-a", marketplace.SubmissionPendingReview, ts)); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	// Defaults: page<1 -> 1, perPage<1 -> 20
	page, total, err := repo.ListPending(ctx, "tenant-a", -3, -1)
	if err != nil {
		t.Fatalf("ListPending defaults: %v", err)
	}
	if total != 5 || len(page) != 5 {
		t.Fatalf("defaults: total=%d len=%d want 5/5", total, len(page))
	}
	// page beyond range -> empty out, total intact
	page, total, err = repo.ListPending(ctx, "tenant-a", 10, 2)
	if err != nil {
		t.Fatalf("ListPending OOB page: %v", err)
	}
	if total != 5 || len(page) != 0 {
		t.Fatalf("OOB page: total=%d len=%d want 5/0", total, len(page))
	}
}

func TestMarketplaceSubmissionsSaveStateUpdatesRow(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	row := makeSubmission("s-save", "tenant-a", marketplace.SubmissionPendingReview, "2026-05-11T10:00:00Z")
	if err := repo.Create(context.Background(), row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	row.State = marketplace.SubmissionApproved
	row.Reviewer = "admin-1"
	row.ReviewNotes = "lgtm"
	if err := repo.SaveState(context.Background(), row); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, err := repo.Get(context.Background(), "s-save")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != marketplace.SubmissionApproved {
		t.Fatalf("State = %q, want approved", got.State)
	}
	if got.Reviewer != "admin-1" {
		t.Fatalf("Reviewer = %q", got.Reviewer)
	}
}

func TestMarketplaceSubmissionsSaveStateMissingReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewMarketplaceSubmissions()
	row := makeSubmission("nope", "tenant-a", marketplace.SubmissionApproved, "2026-05-11T10:00:00Z")
	if err := repo.SaveState(context.Background(), row); !errors.Is(err, marketplace.ErrSubmissionNotFound) {
		t.Fatalf("SaveState missing: err = %v, want ErrSubmissionNotFound", err)
	}
}
