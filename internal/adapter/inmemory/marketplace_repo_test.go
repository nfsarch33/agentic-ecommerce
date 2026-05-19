package inmemory

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/marketplace"
)

func TestMarketplaceCatalogRoundTrip(t *testing.T) {
	t.Parallel()
	cat := NewMarketplaceCatalog()
	ctx := context.Background()
	manifest := marketplace.Manifest{Slug: "stripe-payments", Name: "Stripe", Version: "1.0.0", Vendor: "Stripe"}
	if err := cat.RegisterManifest(ctx, manifest); err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}
	if err := cat.RegisterManifest(ctx, manifest); !errors.Is(err, marketplace.ErrSlugAlreadyExists) {
		t.Fatalf("duplicate should error, got %v", err)
	}
	got, err := cat.GetManifest(ctx, "stripe-payments")
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.Version != manifest.Version {
		t.Fatalf("version mismatch: got %s want %s", got.Version, manifest.Version)
	}
	if _, err := cat.GetManifest(ctx, "ghost"); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("unknown should be ErrPluginNotFound, got %v", err)
	}
	list, total, err := cat.ListManifests(ctx, 1, 10)
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	if total != 1 || len(list) != 1 {
		t.Fatalf("list = %d total %d, want 1", len(list), total)
	}
}

func TestMarketplaceInstallationsCRUD(t *testing.T) {
	t.Parallel()
	repo := NewMarketplaceInstallations()
	ctx := context.Background()
	row := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", InstalledVersion: "1.0.0",
		State: marketplace.StateInstalled, InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z",
	}
	if err := repo.Create(ctx, row); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if err := repo.Create(ctx, row); !errors.Is(err, marketplace.ErrPluginAlreadyInstalled) {
		t.Fatalf("dup Create should error, got %v", err)
	}
	got, err := repo.Get(ctx, "tenant-a", "stripe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.State != marketplace.StateInstalled {
		t.Fatalf("Get state = %s", got.State)
	}
	row.State = marketplace.StateActive
	if err := repo.SaveState(ctx, row); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
	got, _ = repo.Get(ctx, "tenant-a", "stripe")
	if got.State != marketplace.StateActive {
		t.Fatalf("after SaveState got %s", got.State)
	}
	list, total, err := repo.List(ctx, "tenant-a", 1, 10)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("List unexpected: list=%v total=%d err=%v", list, total, err)
	}
	if err := repo.Delete(ctx, "tenant-a", "stripe"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.Get(ctx, "tenant-a", "stripe"); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("Get after Delete should be ErrPluginNotFound, got %v", err)
	}
}

func TestMarketplaceInstallationsTenantIsolation(t *testing.T) {
	t.Parallel()
	repo := NewMarketplaceInstallations()
	ctx := context.Background()
	rowA := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", InstalledVersion: "1.0.0",
		State: marketplace.StateInstalled, InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z",
	}
	rowB := rowA
	rowB.TenantID = "tenant-b"
	if err := repo.Create(ctx, rowA); err != nil {
		t.Fatal(err)
	}
	if err := repo.Create(ctx, rowB); err != nil {
		t.Fatal(err)
	}
	if _, err := repo.Get(ctx, "tenant-a", "stripe"); err != nil {
		t.Fatalf("tenant A: %v", err)
	}
	// Tenant A's listing must not include tenant B.
	list, total, err := repo.List(ctx, "tenant-a", 1, 10)
	if err != nil {
		t.Fatal(err)
	}
	if total != 1 || len(list) != 1 || list[0].TenantID != "tenant-a" {
		t.Fatalf("tenant isolation violated: %v total=%d", list, total)
	}
}

func TestMarketplaceSubscriptionsReplaceList(t *testing.T) {
	t.Parallel()
	repo := NewMarketplaceSubscriptions()
	ctx := context.Background()
	if err := repo.Replace(ctx, "tenant-a", "stripe", []marketplace.EventName{"order.placed", "order.cancelled"}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	got, err := repo.List(ctx, "tenant-a", "stripe")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("List len = %d, want 2", len(got))
	}
	if err := repo.Delete(ctx, "tenant-a", "stripe"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	got, _ = repo.List(ctx, "tenant-a", "stripe")
	if len(got) != 0 {
		t.Fatalf("after Delete len = %d, want 0", len(got))
	}
}
