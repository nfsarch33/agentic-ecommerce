package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/marketplace"
)

func fakeManifestRow(m marketplace.Manifest) fakeRow {
	deps := []byte("[]")
	return fakeRow{values: []any{
		m.Slug, m.Name, m.Version, m.Vendor, m.Description, m.Category, m.HomepageURL,
		[]string{}, []string{}, deps,
	}}
}

func fakeInstallationRow(ins marketplace.Installation) fakeRow {
	at, _ := time.Parse(time.RFC3339Nano, ins.InstalledAt)
	upd, _ := time.Parse(time.RFC3339Nano, ins.UpdatedAt)
	var act *time.Time
	if ins.ActivatedAt != "" {
		t, _ := time.Parse(time.RFC3339Nano, ins.ActivatedAt)
		act = &t
	}
	return fakeRow{values: []any{
		ins.TenantID, ins.Slug, ins.InstalledVersion, string(ins.State),
		at, act, upd,
	}}
}

func TestMarketplaceCatalogRegisterManifestSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &MarketplaceCatalog{pool: pool}
	m := marketplace.Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	if err := repo.RegisterManifest(context.Background(), m); err != nil {
		t.Fatalf("RegisterManifest: %v", err)
	}
	if len(pool.execSQL) != 1 {
		t.Fatalf("expected 1 exec, got %d", len(pool.execSQL))
	}
}

func TestMarketplaceCatalogRegisterManifestRejectsBadManifest(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &MarketplaceCatalog{pool: pool}
	if err := repo.RegisterManifest(context.Background(), marketplace.Manifest{Slug: "x"}); err == nil {
		t.Fatalf("expected validation error for bad manifest")
	}
}

func TestMarketplaceCatalogRegisterManifestUniqueViolation(t *testing.T) {
	t.Parallel()
	pool := &fakePool{execErr: errors.New("ERROR: 23505 duplicate key value")}
	repo := &MarketplaceCatalog{pool: pool}
	m := marketplace.Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	err := repo.RegisterManifest(context.Background(), m)
	if !errors.Is(err, marketplace.ErrSlugAlreadyExists) {
		t.Fatalf("expected ErrSlugAlreadyExists, got %v", err)
	}
}

func TestMarketplaceCatalogGetManifestNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &MarketplaceCatalog{pool: pool}
	if _, err := repo.GetManifest(context.Background(), "ghost"); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarketplaceCatalogGetManifestSuccess(t *testing.T) {
	t.Parallel()
	m := marketplace.Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	pool := &fakePool{row: fakeManifestRow(m)}
	repo := &MarketplaceCatalog{pool: pool}
	got, err := repo.GetManifest(context.Background(), m.Slug)
	if err != nil {
		t.Fatalf("GetManifest: %v", err)
	}
	if got.Slug != m.Slug {
		t.Fatalf("slug = %s", got.Slug)
	}
}

func TestMarketplaceCatalogList(t *testing.T) {
	t.Parallel()
	m := marketplace.Manifest{Slug: "stripe", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	pool := &fakePool{
		row:  fakeRow{values: []any{1}},
		rows: &fakeRows{rows: [][]any{fakeManifestRow(m).values}},
	}
	repo := &MarketplaceCatalog{pool: pool}
	out, total, err := repo.ListManifests(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("ListManifests: %v", err)
	}
	if total != 1 || len(out) != 1 {
		t.Fatalf("got total=%d len=%d", total, len(out))
	}
}

func TestMarketplaceInstallationsCreateSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &MarketplaceInstallations{pool: pool}
	ins := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", InstalledVersion: "1.0.0",
		State:       marketplace.StateInstalled,
		InstalledAt: "2026-05-08T10:00:00Z",
		UpdatedAt:   "2026-05-08T10:00:00Z",
	}
	if err := repo.Create(context.Background(), ins); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestMarketplaceInstallationsCreateUniqueViolation(t *testing.T) {
	t.Parallel()
	pool := &fakePool{execErr: errors.New("ERROR: duplicate key value 23505")}
	repo := &MarketplaceInstallations{pool: pool}
	ins := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", InstalledVersion: "1.0.0",
		State: marketplace.StateInstalled, InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z",
	}
	if err := repo.Create(context.Background(), ins); !errors.Is(err, marketplace.ErrPluginAlreadyInstalled) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarketplaceInstallationsCreateRequiresTenant(t *testing.T) {
	t.Parallel()
	repo := &MarketplaceInstallations{pool: &fakePool{}}
	if err := repo.Create(context.Background(), marketplace.Installation{Slug: "stripe", InstalledVersion: "1.0.0", InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z"}); err == nil {
		t.Fatalf("expected tenant_required error")
	}
}

func TestMarketplaceInstallationsGetNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &MarketplaceInstallations{pool: pool}
	if _, err := repo.Get(context.Background(), "tenant-a", "ghost"); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarketplaceInstallationsGetSuccess(t *testing.T) {
	t.Parallel()
	ins := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", InstalledVersion: "1.0.0",
		State:       marketplace.StateInstalled,
		InstalledAt: "2026-05-08T10:00:00Z",
		UpdatedAt:   "2026-05-08T10:00:00Z",
	}
	pool := &fakePool{row: fakeInstallationRow(ins)}
	repo := &MarketplaceInstallations{pool: pool}
	got, err := repo.Get(context.Background(), "tenant-a", "stripe")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != "stripe" || got.State != marketplace.StateInstalled {
		t.Fatalf("got = %+v", got)
	}
}

func TestMarketplaceInstallationsList(t *testing.T) {
	t.Parallel()
	ins := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", InstalledVersion: "1.0.0",
		State: marketplace.StateInstalled, InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z",
	}
	pool := &fakePool{
		row:  fakeRow{values: []any{1}},
		rows: &fakeRows{rows: [][]any{fakeInstallationRow(ins).values}},
	}
	repo := &MarketplaceInstallations{pool: pool}
	got, total, err := repo.List(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("got total=%d len=%d", total, len(got))
	}
}

func TestMarketplaceInstallationsSaveStateNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &MarketplaceInstallations{pool: pool}
	ins := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", State: marketplace.StateActive,
		InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z",
	}
	if err := repo.SaveState(context.Background(), ins); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarketplaceInstallationsSaveStateSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 1")}
	repo := &MarketplaceInstallations{pool: pool}
	ins := marketplace.Installation{
		TenantID: "tenant-a", Slug: "stripe", State: marketplace.StateActive,
		InstalledAt: "2026-05-08T10:00:00Z", UpdatedAt: "2026-05-08T10:00:00Z",
	}
	if err := repo.SaveState(context.Background(), ins); err != nil {
		t.Fatalf("SaveState: %v", err)
	}
}

func TestMarketplaceInstallationsDeleteNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 0")}
	repo := &MarketplaceInstallations{pool: pool}
	if err := repo.Delete(context.Background(), "tenant-a", "stripe"); !errors.Is(err, marketplace.ErrPluginNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestMarketplaceInstallationsDeleteSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 1")}
	repo := &MarketplaceInstallations{pool: pool}
	if err := repo.Delete(context.Background(), "tenant-a", "stripe"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestMarketplaceSubscriptionsReplace(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 0")}
	repo := &MarketplaceSubscriptions{pool: pool}
	if err := repo.Replace(context.Background(), "tenant-a", "stripe", []marketplace.EventName{"order.placed"}); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if len(pool.execSQL) != 2 { // delete + insert
		t.Fatalf("expected 2 exec, got %d", len(pool.execSQL))
	}
}

func TestMarketplaceSubscriptionsList(t *testing.T) {
	t.Parallel()
	pool := &fakePool{rows: &fakeRows{rows: [][]any{{"order.placed"}, {"order.cancelled"}}}}
	repo := &MarketplaceSubscriptions{pool: pool}
	got, err := repo.List(context.Background(), "tenant-a", "stripe")
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len = %d", len(got))
	}
}

func TestMarketplaceSubscriptionsDelete(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 0")}
	repo := &MarketplaceSubscriptions{pool: pool}
	if err := repo.Delete(context.Background(), "tenant-a", "stripe"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestMarketplaceConstructors(t *testing.T) {
	t.Parallel()
	if NewMarketplaceCatalog(nil) == nil {
		t.Fatalf("NewMarketplaceCatalog returned nil")
	}
	if NewMarketplaceInstallations(nil) == nil {
		t.Fatalf("NewMarketplaceInstallations returned nil")
	}
	if NewMarketplaceSubscriptions(nil) == nil {
		t.Fatalf("NewMarketplaceSubscriptions returned nil")
	}
}

func TestParseRFC3339FallbacksOnEmpty(t *testing.T) {
	t.Parallel()
	got := parseRFC3339("")
	if got.IsZero() {
		t.Fatalf("empty input should fallback to now")
	}
	got = parseRFC3339("2026-05-08T10:00:00Z")
	if got.IsZero() {
		t.Fatalf("non-empty parse failed")
	}
	if got2 := parseRFC3339("garbage"); got2.IsZero() {
		t.Fatalf("garbage input should fallback to now, got zero")
	}
}

func TestParseNullableTime(t *testing.T) {
	t.Parallel()
	if parseNullableTime("") != nil {
		t.Fatalf("empty should be nil")
	}
	if got := parseNullableTime("2026-05-08T10:00:00Z"); got == nil {
		t.Fatalf("non-empty should not be nil")
	}
}

func TestEventNamesAsStrings(t *testing.T) {
	t.Parallel()
	got := eventNamesAsStrings([]marketplace.EventName{"a", "b"})
	if len(got) != 2 || got[0] != "a" || got[1] != "b" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestPermissionsAsStrings(t *testing.T) {
	t.Parallel()
	got := permissionsAsStrings([]marketplace.Permission{"read"})
	if len(got) != 1 || got[0] != "read" {
		t.Fatalf("unexpected: %v", got)
	}
}

func TestIsUniqueViolation(t *testing.T) {
	t.Parallel()
	if !isUniqueViolation(errors.New("23505: duplicate key")) {
		t.Fatalf("23505 should report")
	}
	if isUniqueViolation(nil) {
		t.Fatalf("nil should not report")
	}
	if isUniqueViolation(errors.New("other")) {
		t.Fatalf("other should not report")
	}
}
