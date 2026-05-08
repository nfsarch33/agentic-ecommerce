package marketplace

import (
	"context"
	"errors"
	"testing"
	"time"
)

// stubCatalog and stubInstallations let us drive Service unit tests
// without depending on the inmemory adapter package.
type stubCatalog struct {
	manifests map[string]Manifest
}

func newStubCatalog(manifests ...Manifest) *stubCatalog {
	c := &stubCatalog{manifests: make(map[string]Manifest)}
	for _, m := range manifests {
		c.manifests[m.Slug] = m
	}
	return c
}

func (c *stubCatalog) RegisterManifest(_ context.Context, m Manifest) error {
	if _, ok := c.manifests[m.Slug]; ok {
		return ErrSlugAlreadyExists
	}
	c.manifests[m.Slug] = m
	return nil
}
func (c *stubCatalog) GetManifest(_ context.Context, slug string) (Manifest, error) {
	m, ok := c.manifests[slug]
	if !ok {
		return Manifest{}, ErrPluginNotFound
	}
	return m, nil
}
func (c *stubCatalog) ListManifests(_ context.Context, _, _ int) ([]Manifest, int, error) {
	out := make([]Manifest, 0, len(c.manifests))
	for _, m := range c.manifests {
		out = append(out, m)
	}
	return out, len(out), nil
}

type stubInstallations struct {
	rows map[string]Installation
}

func newStubInstallations() *stubInstallations {
	return &stubInstallations{rows: make(map[string]Installation)}
}
func key(tenant, slug string) string { return tenant + "::" + slug }

func (r *stubInstallations) Create(_ context.Context, ins Installation) error {
	if _, ok := r.rows[key(ins.TenantID, ins.Slug)]; ok {
		return ErrPluginAlreadyInstalled
	}
	r.rows[key(ins.TenantID, ins.Slug)] = ins
	return nil
}
func (r *stubInstallations) Get(_ context.Context, tenantID, slug string) (Installation, error) {
	row, ok := r.rows[key(tenantID, slug)]
	if !ok {
		return Installation{}, ErrPluginNotFound
	}
	return row, nil
}
func (r *stubInstallations) List(_ context.Context, _ string, _, _ int) ([]Installation, int, error) {
	out := make([]Installation, 0, len(r.rows))
	for _, ins := range r.rows {
		out = append(out, ins)
	}
	return out, len(out), nil
}
func (r *stubInstallations) SaveState(_ context.Context, ins Installation) error {
	if _, ok := r.rows[key(ins.TenantID, ins.Slug)]; !ok {
		return ErrPluginNotFound
	}
	r.rows[key(ins.TenantID, ins.Slug)] = ins
	return nil
}
func (r *stubInstallations) Delete(_ context.Context, tenantID, slug string) error {
	if _, ok := r.rows[key(tenantID, slug)]; !ok {
		return ErrPluginNotFound
	}
	delete(r.rows, key(tenantID, slug))
	return nil
}

// stubSubs records replace + delete calls so tests can assert
// subscription churn.
type stubSubs struct {
	replaceCalls int
	deleteCalls  int
}

func (s *stubSubs) Replace(_ context.Context, _, _ string, _ []EventName) error {
	s.replaceCalls++
	return nil
}
func (s *stubSubs) List(_ context.Context, _, _ string) ([]EventName, error) { return nil, nil }
func (s *stubSubs) Delete(_ context.Context, _, _ string) error              { s.deleteCalls++; return nil }

func newTestService(t *testing.T, catalog *stubCatalog, ins *stubInstallations, subs SubscriptionRepository) *Service {
	t.Helper()
	clock := func() string { return "2026-05-08T10:00:00Z" }
	svc, err := NewService(ServiceConfig{
		Catalog:       catalog,
		Installations: ins,
		Subscriptions: subs,
		Clock:         clock,
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	return svc
}

// TestLifecycleHappyPath drives install -> activate -> deactivate ->
// activate -> uninstall and verifies the row state at each step.
func TestLifecycleHappyPath(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.2.3", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	ins := newStubInstallations()
	subs := &stubSubs{}
	svc := newTestService(t, cat, ins, subs)
	ctx := context.Background()

	row, err := svc.Install(ctx, "tenant-a", manifest)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if row.State != StateInstalled {
		t.Fatalf("State after Install = %s, want installed", row.State)
	}
	if subs.replaceCalls != 1 {
		t.Fatalf("Replace should be called once on Install, got %d", subs.replaceCalls)
	}

	row, err = svc.Activate(ctx, "tenant-a", manifest.Slug)
	if err != nil {
		t.Fatalf("Activate: %v", err)
	}
	if row.State != StateActive {
		t.Fatalf("State after Activate = %s, want active", row.State)
	}
	if row.ActivatedAt == "" {
		t.Fatalf("ActivatedAt should be set after first Activate")
	}

	row, err = svc.Deactivate(ctx, "tenant-a", manifest.Slug)
	if err != nil {
		t.Fatalf("Deactivate: %v", err)
	}
	if row.State != StateDeactivated {
		t.Fatalf("State after Deactivate = %s, want deactivated", row.State)
	}

	row, err = svc.Activate(ctx, "tenant-a", manifest.Slug)
	if err != nil {
		t.Fatalf("Activate from deactivated: %v", err)
	}
	if row.State != StateActive {
		t.Fatalf("re-Activate State = %s, want active", row.State)
	}

	if err := svc.Uninstall(ctx, "tenant-a", manifest.Slug); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if subs.deleteCalls != 1 {
		t.Fatalf("Delete should be called once on Uninstall, got %d", subs.deleteCalls)
	}
	if _, err := svc.Get(ctx, "tenant-a", manifest.Slug); !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("Get after Uninstall should be ErrPluginNotFound, got %v", err)
	}
}

func TestInstallRejectsDuplicate(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	ins := newStubInstallations()
	svc := newTestService(t, cat, ins, &stubSubs{})
	ctx := context.Background()

	if _, err := svc.Install(ctx, "tenant-a", manifest); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	_, err := svc.Install(ctx, "tenant-a", manifest)
	if !errors.Is(err, ErrPluginAlreadyInstalled) {
		t.Fatalf("expected ErrPluginAlreadyInstalled, got %v", err)
	}
}

func TestInstallRequiresCatalogEntry(t *testing.T) {
	t.Parallel()
	cat := newStubCatalog()
	svc := newTestService(t, cat, newStubInstallations(), &stubSubs{})
	manifest := Manifest{Slug: "ghost", Name: "Ghost", Version: "1.0.0", Vendor: "Acme"}
	_, err := svc.Install(context.Background(), "tenant-a", manifest)
	if !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound, got %v", err)
	}
}

func TestActivateInvalidTransition(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	ins := newStubInstallations()
	svc := newTestService(t, cat, ins, &stubSubs{})
	ctx := context.Background()
	if _, err := svc.Install(ctx, "tenant-a", manifest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := svc.Activate(ctx, "tenant-a", manifest.Slug); err != nil {
		t.Fatalf("Activate: %v", err)
	}
	// Activate from active is a no-op state transition: nextState
	// returns ErrInvalidTransition because the table does not allow
	// activate -> active (legitimate idempotency would loop, but the
	// state machine intentionally rejects it so callers cannot
	// accidentally drift the activated_at timestamp).
	if _, err := svc.Activate(ctx, "tenant-a", manifest.Slug); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for active->activate, got %v", err)
	}
}

func TestInstallRequiresTenant(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	svc := newTestService(t, cat, newStubInstallations(), &stubSubs{})
	if _, err := svc.Install(context.Background(), "  ", manifest); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected ErrManifestInvalid, got %v", err)
	}
}

func TestSandboxExhaustionBlocksTransition(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	ins := newStubInstallations()
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	clock := func() time.Time { return now }
	// HookBudget=1 lets the first activate succeed; the next
	// transition (deactivate) exhausts the bucket.
	sb := NewSandbox(SandboxConfig{HookBudget: 1, Window: time.Hour, HookTimeout: time.Second, Now: clock})
	svc, err := NewService(ServiceConfig{
		Catalog:       cat,
		Installations: ins,
		Sandbox:       sb,
		Clock:         func() string { return now.Format(time.RFC3339Nano) },
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	if _, err := svc.Install(context.Background(), "tenant-a", manifest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	if _, err := svc.Activate(context.Background(), "tenant-a", manifest.Slug); err != nil {
		t.Fatalf("first Activate should succeed within budget: %v", err)
	}
	_, err = svc.Deactivate(context.Background(), "tenant-a", manifest.Slug)
	if !errors.Is(err, ErrSandboxBudgetExceeded) {
		t.Fatalf("expected ErrSandboxBudgetExceeded after budget exhaustion, got %v", err)
	}
}

func TestSettingsRoundTrip(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	svc := newTestService(t, cat, newStubInstallations(), &stubSubs{})
	svc.SetSettings("tenant-a", "stripe-payments", map[string]any{"webhook": "https://example.com"})
	got := svc.Settings("tenant-a", "stripe-payments")
	if got["webhook"] != "https://example.com" {
		t.Fatalf("settings round-trip failed: %v", got)
	}
	// Unknown installation returns empty map.
	if other := svc.Settings("tenant-other", "stripe-payments"); len(other) != 0 {
		t.Fatalf("settings should be tenant-scoped, got %v", other)
	}
}

func TestNewServiceConfigErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewService(ServiceConfig{}); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("missing catalog should error, got %v", err)
	}
	if _, err := NewService(ServiceConfig{Catalog: newStubCatalog()}); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("missing installations should error, got %v", err)
	}
}

func TestServiceCatalogAndSandboxAccessors(t *testing.T) {
	t.Parallel()
	cat := newStubCatalog()
	svc := newTestService(t, cat, newStubInstallations(), &stubSubs{})
	if svc.Catalog() == nil {
		t.Fatalf("Catalog() should not be nil")
	}
	if svc.Sandbox() == nil {
		t.Fatalf("Sandbox() should not be nil")
	}
}

func TestServiceListSurfacesTenantValidation(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newStubCatalog(), newStubInstallations(), &stubSubs{})
	if _, _, err := svc.List(context.Background(), "  ", 1, 10); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected tenant validation, got %v", err)
	}
}

func TestServiceUninstallNotFound(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newStubCatalog(), newStubInstallations(), &stubSubs{})
	if err := svc.Uninstall(context.Background(), "tenant-a", "ghost"); !errors.Is(err, ErrPluginNotFound) {
		t.Fatalf("expected ErrPluginNotFound, got %v", err)
	}
}

func TestServiceListReturnsRows(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	ins := newStubInstallations()
	svc := newTestService(t, cat, ins, &stubSubs{})
	if _, err := svc.Install(context.Background(), "tenant-a", manifest); err != nil {
		t.Fatalf("Install: %v", err)
	}
	rows, total, err := svc.List(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("List unexpected: rows=%v total=%d", rows, total)
	}
}

func TestServiceInstallRejectsBadManifest(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newStubCatalog(), newStubInstallations(), &stubSubs{})
	bad := Manifest{Slug: "INVALID", Name: "N", Version: "1.0.0", Vendor: "V"}
	if _, err := svc.Install(context.Background(), "tenant-a", bad); !errors.Is(err, ErrSlugInvalid) {
		t.Fatalf("expected ErrSlugInvalid, got %v", err)
	}
}

func TestServiceInstallReinstallAfterUninstall(t *testing.T) {
	t.Parallel()
	manifest := Manifest{Slug: "stripe-payments", Name: "S", Version: "1.0.0", Vendor: "Stripe"}
	cat := newStubCatalog(manifest)
	ins := newStubInstallations()
	svc := newTestService(t, cat, ins, &stubSubs{})
	ctx := context.Background()
	if _, err := svc.Install(ctx, "tenant-a", manifest); err != nil {
		t.Fatalf("first Install: %v", err)
	}
	if err := svc.Uninstall(ctx, "tenant-a", manifest.Slug); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := svc.Install(ctx, "tenant-a", manifest); err != nil {
		t.Fatalf("re-Install after Uninstall should succeed: %v", err)
	}
}

func TestServiceUninstallRequiresTenantSlug(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newStubCatalog(), newStubInstallations(), &stubSubs{})
	if err := svc.Uninstall(context.Background(), "tenant-a", "INVALID"); !errors.Is(err, ErrSlugInvalid) {
		t.Fatalf("expected ErrSlugInvalid, got %v", err)
	}
}

func TestServiceGetReturnsErrors(t *testing.T) {
	t.Parallel()
	svc := newTestService(t, newStubCatalog(), newStubInstallations(), &stubSubs{})
	if _, err := svc.Get(context.Background(), "  ", "stripe"); !errors.Is(err, ErrManifestInvalid) {
		t.Fatalf("expected tenant validation, got %v", err)
	}
	if _, err := svc.Get(context.Background(), "tenant-a", "INVALID"); !errors.Is(err, ErrSlugInvalid) {
		t.Fatalf("expected ErrSlugInvalid, got %v", err)
	}
}

func TestNoopSubscriptionRepo(t *testing.T) {
	t.Parallel()
	repo := noopSubscriptionRepo{}
	if err := repo.Replace(context.Background(), "t", "s", nil); err != nil {
		t.Fatalf("Replace: %v", err)
	}
	if _, err := repo.List(context.Background(), "t", "s"); err != nil {
		t.Fatalf("List: %v", err)
	}
	if err := repo.Delete(context.Background(), "t", "s"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
}

func TestDefaultClock(t *testing.T) {
	t.Parallel()
	if defaultClock() == "" {
		t.Fatalf("defaultClock should return non-empty string")
	}
}
