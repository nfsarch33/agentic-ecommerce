package marketplace_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

func mkValidManifest() marketplace.Manifest {
	return marketplace.Manifest{
		Slug:    "stripe-payments",
		Name:    "Stripe Payments",
		Version: "1.0.0",
		Vendor:  "Acme Labs",
	}
}

func newServiceForTest(t *testing.T) (*marketplace.SubmissionService, *inmemory.MarketplaceSubmissions, *inmemory.MarketplaceCatalog) {
	t.Helper()
	subs := inmemory.NewMarketplaceSubmissions()
	cat := inmemory.NewMarketplaceCatalog()
	clock := func() string { return "2026-05-09T00:00:00Z" }
	svc, err := marketplace.NewSubmissionService(marketplace.SubmissionServiceConfig{
		Submissions: subs, Catalog: cat, Clock: clock,
	})
	if err != nil {
		t.Fatalf("NewSubmissionService: %v", err)
	}
	return svc, subs, cat
}

func TestSubmissionService_NewService_RejectsMissingDeps(t *testing.T) {
	t.Parallel()
	if _, err := marketplace.NewSubmissionService(marketplace.SubmissionServiceConfig{}); !errors.Is(err, marketplace.ErrSubmissionInvalid) {
		t.Fatalf("missing deps err = %v", err)
	}
	if _, err := marketplace.NewSubmissionService(marketplace.SubmissionServiceConfig{Submissions: inmemory.NewMarketplaceSubmissions()}); !errors.Is(err, marketplace.ErrSubmissionInvalid) {
		t.Fatalf("missing catalog err = %v", err)
	}
}

func TestSubmissionService_Submit(t *testing.T) {
	t.Parallel()
	svc, subs, _ := newServiceForTest(t)
	row, err := svc.Submit(context.Background(), "tenant-acme", "vendor@acme.example", "sub-1", mkValidManifest())
	if err != nil {
		t.Fatalf("Submit: %v", err)
	}
	if row.State != marketplace.SubmissionPendingReview {
		t.Fatalf("state=%s want pending_review", row.State)
	}
	if row.SubmittedAt != "2026-05-09T00:00:00Z" {
		t.Fatalf("submittedAt=%s", row.SubmittedAt)
	}
	stored, err := subs.Get(context.Background(), "sub-1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if stored.ID != "sub-1" {
		t.Fatalf("stored.ID=%s", stored.ID)
	}
}

func TestSubmissionService_Submit_RejectsInvalid(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	cases := []struct {
		name        string
		id          string
		tenantID    string
		email       string
		manifestMut func(m *marketplace.Manifest)
	}{
		{"empty id", "", "tenant-a", "v@a.example", nil},
		{"empty tenant", "sub-1", "", "v@a.example", nil},
		{"bad email", "sub-1", "tenant-a", "not-an-email", nil},
		{"bad manifest", "sub-1", "tenant-a", "v@a.example", func(m *marketplace.Manifest) { m.Slug = "" }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := mkValidManifest()
			if tc.manifestMut != nil {
				tc.manifestMut(&m)
			}
			_, err := svc.Submit(context.Background(), tc.tenantID, tc.email, tc.id, m)
			if !errors.Is(err, marketplace.ErrSubmissionInvalid) {
				t.Fatalf("err=%v want ErrSubmissionInvalid", err)
			}
		})
	}
}

func TestSubmissionService_Submit_DuplicateID(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest())
	if !errors.Is(err, marketplace.ErrSubmissionAlreadyExists) {
		t.Fatalf("err=%v want ErrSubmissionAlreadyExists", err)
	}
}

func TestSubmissionService_Approve(t *testing.T) {
	t.Parallel()
	svc, _, cat := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Approve(context.Background(), "sub-1", "admin@example", "looks good")
	if err != nil {
		t.Fatalf("Approve: %v", err)
	}
	if row.State != marketplace.SubmissionApproved {
		t.Fatalf("state=%s want approved", row.State)
	}
	if row.Reviewer != "admin@example" {
		t.Fatalf("reviewer=%s", row.Reviewer)
	}
	// Manifest should now exist in the catalog.
	got, err := cat.GetManifest(context.Background(), "stripe-payments")
	if err != nil {
		t.Fatalf("catalog should have manifest: %v", err)
	}
	if got.Version != "1.0.0" {
		t.Fatalf("manifest version=%s", got.Version)
	}
}

func TestSubmissionService_Reject(t *testing.T) {
	t.Parallel()
	svc, _, cat := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	row, err := svc.Reject(context.Background(), "sub-1", "admin@example", "missing license")
	if err != nil {
		t.Fatalf("Reject: %v", err)
	}
	if row.State != marketplace.SubmissionRejected {
		t.Fatalf("state=%s want rejected", row.State)
	}
	// Manifest should NOT be in the catalog after rejection.
	if _, err := cat.GetManifest(context.Background(), "stripe-payments"); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("catalog must not register rejected manifest, got %v", err)
	}
}

func TestSubmissionService_Approve_TerminalRejected(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Reject(context.Background(), "sub-1", "admin@example", "no"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Approve(context.Background(), "sub-1", "admin@example", "")
	if !errors.Is(err, marketplace.ErrInvalidTransition) {
		t.Fatalf("approving terminal rejected should fail; err=%v", err)
	}
}

func TestSubmissionService_Reject_TerminalApproved(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Approve(context.Background(), "sub-1", "admin@example", "ok"); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Reject(context.Background(), "sub-1", "admin@example", "")
	if !errors.Is(err, marketplace.ErrInvalidTransition) {
		t.Fatalf("rejecting terminal approved should fail; err=%v", err)
	}
}

func TestSubmissionService_Get_NotFound(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	_, err := svc.Get(context.Background(), "missing")
	if !errors.Is(err, marketplace.ErrSubmissionNotFound) {
		t.Fatalf("err=%v", err)
	}
}

func TestSubmissionService_Approve_RequiresReviewer(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	_, err := svc.Approve(context.Background(), "sub-1", "", "")
	if !errors.Is(err, marketplace.ErrSubmissionInvalid) {
		t.Fatalf("err=%v want invalid for empty reviewer", err)
	}
}

func TestSubmissionService_ListPending(t *testing.T) {
	t.Parallel()
	svc, _, _ := newServiceForTest(t)
	if _, err := svc.Submit(context.Background(), "tenant-a", "v@a.example", "sub-a-1", mkValidManifest()); err != nil {
		t.Fatal(err)
	}
	mB := mkValidManifest()
	mB.Slug = "ses-email"
	if _, err := svc.Submit(context.Background(), "tenant-b", "v@b.example", "sub-b-1", mB); err != nil {
		t.Fatal(err)
	}
	// Tenant scope filters.
	rows, total, err := svc.ListPending(context.Background(), "tenant-a", 1, 10)
	if err != nil || total != 1 || len(rows) != 1 || rows[0].TenantID != "tenant-a" {
		t.Fatalf("scoped list rows=%v total=%d err=%v", rows, total, err)
	}
	// Cross-tenant view returns both rows.
	rows, total, err = svc.ListPending(context.Background(), "", 1, 10)
	if err != nil || total != 2 || len(rows) != 2 {
		t.Fatalf("cross-tenant list rows=%v total=%d err=%v", rows, total, err)
	}
}

func TestSubmissionTransition_Lookups(t *testing.T) {
	t.Parallel()
	if _, err := marketplace.ParseSubmissionState("approved"); err != nil {
		t.Fatalf("parse approved: %v", err)
	}
	if _, err := marketplace.ParseSubmissionState("garbage"); !errors.Is(err, marketplace.ErrInvalidTransition) {
		t.Fatalf("parse garbage: %v", err)
	}
	if _, err := marketplace.ParseSubmissionTransition("approve"); err != nil {
		t.Fatalf("parse approve: %v", err)
	}
	if _, err := marketplace.ParseSubmissionTransition("delete"); !errors.Is(err, marketplace.ErrInvalidTransitionName) {
		t.Fatalf("parse delete: %v", err)
	}
	if got := marketplace.SubmissionApproved.IsTerminal(); !got {
		t.Fatalf("approved must be terminal")
	}
	if got := marketplace.SubmissionRejected.IsTerminal(); !got {
		t.Fatalf("rejected must be terminal")
	}
	if got := marketplace.SubmissionPendingReview.IsTerminal(); got {
		t.Fatalf("pending_review must be non-terminal")
	}
	triples := marketplace.AllSubmissionTransitions()
	if len(triples) != 2 {
		t.Fatalf("expected 2 triples, got %d", len(triples))
	}
}
