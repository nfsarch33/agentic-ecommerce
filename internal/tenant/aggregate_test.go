package tenant

import (
	"context"
	"errors"
	"testing"
	"time"
)

func newTestAggregateService(t *testing.T) *AggregateService {
	t.Helper()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	repo := NewInMemoryAggregateRepository()
	svc := NewAggregateService(repo).withClock(func() time.Time { return now })
	return svc
}

func TestCreateTenantHappyPath(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	got, err := svc.Create(context.Background(), CreateTenantInput{
		Slug: "acme", Name: "Acme",
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Status != StatusProvisioning {
		t.Fatalf("Status = %s, want provisioning", got.Status)
	}
	if got.Slug != "acme" {
		t.Fatalf("Slug = %s, want acme", got.Slug)
	}
	if got.Plan != "free" {
		t.Fatalf("Plan default = %s, want free", got.Plan)
	}
}

func TestCreateRejectsDuplicateSlug(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	if _, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("first Create: %v", err)
	}
	_, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme", Name: "Acme 2"})
	if !errors.Is(err, ErrTenantSlugAlreadyExists) {
		t.Fatalf("expected ErrTenantSlugAlreadyExists, got %v", err)
	}
}

func TestCreateRejectsBadSlug(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	_, err := svc.Create(context.Background(), CreateTenantInput{Slug: "ACME", Name: "Acme"})
	if !errors.Is(err, ErrTenantSlugInvalid) {
		t.Fatalf("expected ErrTenantSlugInvalid, got %v", err)
	}
	_, err = svc.Create(context.Background(), CreateTenantInput{Slug: "a", Name: "A"})
	if !errors.Is(err, ErrTenantSlugInvalid) {
		t.Fatalf("expected ErrTenantSlugInvalid for single-char slug, got %v", err)
	}
	_, err = svc.Create(context.Background(), CreateTenantInput{Slug: "ok", Name: "  "})
	if !errors.Is(err, ErrTenantSlugInvalid) {
		t.Fatalf("expected ErrTenantSlugInvalid for empty name, got %v", err)
	}
}

func TestCreateRespectsQuota(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t).WithQuota(2)
	if _, err := svc.Create(context.Background(), CreateTenantInput{Slug: "one", Name: "One"}); err != nil {
		t.Fatalf("first: %v", err)
	}
	if _, err := svc.Create(context.Background(), CreateTenantInput{Slug: "two", Name: "Two"}); err != nil {
		t.Fatalf("second: %v", err)
	}
	_, err := svc.Create(context.Background(), CreateTenantInput{Slug: "three", Name: "Three"})
	if !errors.Is(err, ErrTenantQuotaExceeded) {
		t.Fatalf("expected ErrTenantQuotaExceeded, got %v", err)
	}
}

func TestStatusTransitions(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	tnt, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tnt, err = svc.Activate(context.Background(), tnt.ID)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if tnt.Status != StatusActive {
		t.Fatalf("Activate -> %s, want active", tnt.Status)
	}
	tnt, err = svc.Suspend(context.Background(), tnt.ID)
	if err != nil {
		t.Fatalf("Suspend: %v", err)
	}
	if tnt.Status != StatusSuspended {
		t.Fatalf("Suspend -> %s, want suspended", tnt.Status)
	}
	tnt, err = svc.Activate(context.Background(), tnt.ID)
	if err != nil {
		t.Fatalf("Re-Activate: %v", err)
	}
	if tnt.Status != StatusActive {
		t.Fatalf("re-Activate -> %s, want active", tnt.Status)
	}
	tnt, err = svc.Archive(context.Background(), tnt.ID)
	if err != nil {
		t.Fatalf("Archive: %v", err)
	}
	if tnt.Status != StatusArchived {
		t.Fatalf("Archive -> %s, want archived", tnt.Status)
	}
	if _, err := svc.Activate(context.Background(), tnt.ID); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("activate-from-archived should error, got %v", err)
	}
}

func TestActivateInvalidFromActive(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	tnt, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if _, err := svc.Activate(context.Background(), tnt.ID); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if _, err := svc.Activate(context.Background(), tnt.ID); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("activate-from-active should error, got %v", err)
	}
}

func TestUpdateNameAndPlan(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	tnt, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	tnt, err = svc.Update(context.Background(), tnt.ID, "Acme Renamed", "pro")
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if tnt.Name != "Acme Renamed" {
		t.Fatalf("Name = %s", tnt.Name)
	}
	if tnt.Plan != "pro" {
		t.Fatalf("Plan = %s", tnt.Plan)
	}
}

func TestGetByIDAndSlug(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	tnt, err := svc.Create(context.Background(), CreateTenantInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := svc.Get(context.Background(), tnt.ID)
	if err != nil {
		t.Fatalf("Get by ID: %v", err)
	}
	if got.Slug != "acme" {
		t.Fatalf("Slug = %s", got.Slug)
	}
	got2, err := svc.GetBySlug(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got2.ID != tnt.ID {
		t.Fatalf("GetBySlug id = %s, want %s", got2.ID, tnt.ID)
	}
	if _, err := svc.GetBySlug(context.Background(), "ACME"); !errors.Is(err, ErrTenantSlugInvalid) {
		t.Fatalf("invalid slug should error, got %v", err)
	}
}

func TestListPagination(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	for _, slug := range []string{"one", "two", "three"} {
		if _, err := svc.Create(context.Background(), CreateTenantInput{Slug: slug, Name: slug}); err != nil {
			t.Fatalf("seed %s: %v", slug, err)
		}
	}
	tenants, total, err := svc.List(context.Background(), 1, 2)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 3 {
		t.Fatalf("total = %d, want 3", total)
	}
	if len(tenants) != 2 {
		t.Fatalf("page size = %d, want 2", len(tenants))
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()
	if _, err := ParseStatus("active"); err != nil {
		t.Fatalf("ParseStatus active: %v", err)
	}
	if _, err := ParseStatus("garbage"); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("ParseStatus garbage: %v", err)
	}
}

func TestStatusIsTerminal(t *testing.T) {
	t.Parallel()
	if !StatusArchived.IsTerminal() {
		t.Fatalf("archived must be terminal")
	}
	if StatusActive.IsTerminal() {
		t.Fatalf("active must not be terminal")
	}
}

func TestIsValidSlug(t *testing.T) {
	t.Parallel()
	cases := map[string]bool{
		"acme":      true,
		"acme-corp": true,
		"acme123":   true,
		"a":         false,
		"-acme":     false,
		"acme-":     false,
		"ACME":      false,
		"acme corp": false,
	}
	for slug, want := range cases {
		if got := IsValidSlug(slug); got != want {
			t.Fatalf("IsValidSlug(%q) = %v, want %v", slug, got, want)
		}
	}
}

func TestNormaliseInputIDDefaultsToSlug(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	got, err := svc.Create(context.Background(), CreateTenantInput{Slug: "alpha", Name: "Alpha"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(got.ID) != "alpha" {
		t.Fatalf("ID should default to slug, got %s", got.ID)
	}
}

func TestExplicitInputIDPreserved(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	got, err := svc.Create(context.Background(), CreateTenantInput{ID: "explicit-id", Slug: "alpha", Name: "Alpha"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if string(got.ID) != "explicit-id" {
		t.Fatalf("ID = %s, want explicit-id", got.ID)
	}
}

func TestDefaultPlanFreeFallback(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	got, err := svc.Create(context.Background(), CreateTenantInput{Slug: "alpha", Name: "Alpha", Plan: "  "})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if got.Plan != "free" {
		t.Fatalf("plan = %s, want free", got.Plan)
	}
}

func TestUpdateNoOpFields(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	tnt, _ := svc.Create(context.Background(), CreateTenantInput{Slug: "alpha", Name: "Alpha"})
	tnt2, err := svc.Update(context.Background(), tnt.ID, "", "")
	if err != nil {
		t.Fatalf("Update no-op: %v", err)
	}
	if tnt2.Name != "Alpha" {
		t.Fatalf("Name should be unchanged, got %s", tnt2.Name)
	}
	if tnt2.Plan != "free" {
		t.Fatalf("Plan should be unchanged, got %s", tnt2.Plan)
	}
}

func TestGetEmptyIDRejected(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	if _, err := svc.Get(context.Background(), ""); !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("expected ErrTenantRequired, got %v", err)
	}
}

func TestActivateMissingTenant(t *testing.T) {
	t.Parallel()
	svc := newTestAggregateService(t)
	if _, err := svc.Activate(context.Background(), "ghost"); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("expected ErrTenantNotFound, got %v", err)
	}
}

func TestInMemoryListPagination(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryAggregateRepository()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	for i, slug := range []string{"alpha", "beta", "gamma"} {
		t1 := Tenant{
			ID: ID(slug), Slug: slug, Name: slug, Plan: "free",
			Status:    StatusProvisioning,
			CreatedAt: now.Add(time.Duration(i) * time.Minute),
			UpdatedAt: now.Add(time.Duration(i) * time.Minute),
		}
		if err := repo.Create(context.Background(), t1); err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := repo.List(context.Background(), 1, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 2 {
		t.Fatalf("total=%d len=%d", total, len(rows))
	}
	rows, total, err = repo.List(context.Background(), 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if total != 3 || len(rows) != 1 {
		t.Fatalf("page 2: total=%d len=%d", total, len(rows))
	}
}

func TestInMemorySaveStatusNotFound(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryAggregateRepository()
	tnt := Tenant{ID: "ghost", Slug: "ghost", Name: "G", Plan: "free", Status: StatusActive}
	if err := repo.SaveStatus(context.Background(), tnt); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("expected ErrTenantNotFound, got %v", err)
	}
	if err := repo.Update(context.Background(), tnt); !errors.Is(err, ErrTenantNotFound) {
		t.Fatalf("Update on missing should err, got %v", err)
	}
}

func TestInMemoryDuplicateID(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryAggregateRepository()
	tnt := Tenant{ID: "alpha", Slug: "alpha", Name: "A", Status: StatusProvisioning}
	if err := repo.Create(context.Background(), tnt); err != nil {
		t.Fatal(err)
	}
	tnt2 := tnt
	tnt2.Slug = "different-slug"
	if err := repo.Create(context.Background(), tnt2); err == nil {
		t.Fatalf("expected dup id error")
	}
}
