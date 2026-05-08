package marketplace

import (
	"context"
	"fmt"
	"strings"
)

// CatalogRepository persists Manifest rows that describe what plugins
// are installable on the marketplace. The catalogue is global (shared
// across tenants) -- the per-tenant slice lives in the Installation
// repository.
type CatalogRepository interface {
	// RegisterManifest stores a new manifest in the catalogue. Returns
	// ErrSlugAlreadyExists when slug duplicates another row.
	RegisterManifest(ctx context.Context, m Manifest) error

	// GetManifest returns the manifest for a slug. Returns
	// ErrPluginNotFound when missing.
	GetManifest(ctx context.Context, slug string) (Manifest, error)

	// ListManifests returns paginated manifests, optionally filtered.
	ListManifests(ctx context.Context, page, perPage int) ([]Manifest, int, error)
}

// InstallationRepository persists per-tenant per-slug Installation
// rows. Every method is tenant-aware so adapters can never accidentally
// cross tenant boundaries -- the digital v2.3.0 pattern.
type InstallationRepository interface {
	Create(ctx context.Context, ins Installation) error
	Get(ctx context.Context, tenantID, slug string) (Installation, error)
	List(ctx context.Context, tenantID string, page, perPage int) ([]Installation, int, error)
	SaveState(ctx context.Context, ins Installation) error
	Delete(ctx context.Context, tenantID, slug string) error
}

// SubscriptionRepository persists the per-tenant per-plugin event
// subscription rows. v2.4.0 stores the subscription set so a host
// process can rebuild the bus on restart without re-reading every
// manifest.
type SubscriptionRepository interface {
	Replace(ctx context.Context, tenantID, slug string, events []EventName) error
	List(ctx context.Context, tenantID, slug string) ([]EventName, error)
	Delete(ctx context.Context, tenantID, slug string) error
}

// Registry is the orchestration port. Concrete implementations live in
// internal/marketplace as `Service`. The Registry validates manifests,
// drives the state machine, runs sandboxed lifecycle hooks, and
// persists every transition.
type Registry interface {
	Install(ctx context.Context, tenantID string, manifest Manifest) (Installation, error)
	Activate(ctx context.Context, tenantID, slug string) (Installation, error)
	Deactivate(ctx context.Context, tenantID, slug string) (Installation, error)
	Uninstall(ctx context.Context, tenantID, slug string) error
	List(ctx context.Context, tenantID string, page, perPage int) ([]Installation, int, error)
	Get(ctx context.Context, tenantID, slug string) (Installation, error)
}

// Clock is the time source used by the registry. Tests inject a fixed
// clock; production wiring uses time.Now.
type Clock func() string // RFC3339 timestamp

// ServiceConfig wires the dependencies of the default Registry impl.
type ServiceConfig struct {
	Catalog       CatalogRepository
	Installations InstallationRepository
	Subscriptions SubscriptionRepository
	Sandbox       *Sandbox
	Clock         Clock
}

// Service is the default Registry implementation. It mirrors the
// internal/digital `Service` shape so wiring stays familiar.
type Service struct {
	cat      CatalogRepository
	ins      InstallationRepository
	subs     SubscriptionRepository
	sb       *Sandbox
	clock    Clock
	settings *settingsStore
}

// NewService validates the config and returns a Service. Required
// fields are: Catalog, Installations. Subscriptions is optional (a
// no-op repository is used when omitted). Sandbox falls back to a
// permissive default.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Catalog == nil {
		return nil, fmt.Errorf("%w: catalog repository missing", ErrManifestInvalid)
	}
	if cfg.Installations == nil {
		return nil, fmt.Errorf("%w: installations repository missing", ErrManifestInvalid)
	}
	clock := cfg.Clock
	if clock == nil {
		clock = defaultClock
	}
	subs := cfg.Subscriptions
	if subs == nil {
		subs = noopSubscriptionRepo{}
	}
	sandbox := cfg.Sandbox
	if sandbox == nil {
		sandbox = NewPermissiveSandbox()
	}
	return &Service{
		cat:      cfg.Catalog,
		ins:      cfg.Installations,
		subs:     subs,
		sb:       sandbox,
		clock:    clock,
		settings: newSettingsStore(),
	}, nil
}

// Catalog exposes the underlying CatalogRepository so HTTP handlers
// can serve /marketplace/plugins listings without cracking the
// service open.
func (s *Service) Catalog() CatalogRepository { return s.cat }

// Settings returns a defensive copy of the per-(tenant, slug) settings
// blob. An unknown installation returns an empty map.
func (s *Service) Settings(tenantID, slug string) map[string]any {
	return s.settings.get(tenantID, slug)
}

// SetSettings overwrites the per-(tenant, slug) settings blob.
// Defensive copy is taken inside the store to keep the caller's map
// independent from the canonical state.
func (s *Service) SetSettings(tenantID, slug string, values map[string]any) {
	s.settings.set(tenantID, slug, values)
}

// Sandbox returns the configured sandbox so wiring code can interact
// with it (e.g. tests verifying budget exhaustion).
func (s *Service) Sandbox() *Sandbox { return s.sb }

// Install registers a per-tenant installation row in the StateInstalled
// state and persists the manifest's event subscriptions. The manifest
// MUST already be present in the catalogue; the catalogue is loaded
// once at boot per the v2.4.0 spec, and Install is the per-tenant
// adoption flow.
func (s *Service) Install(ctx context.Context, tenantID string, manifest Manifest) (Installation, error) {
	if err := requireTenant(tenantID); err != nil {
		return Installation{}, err
	}
	if err := manifest.Validate(); err != nil {
		return Installation{}, err
	}
	if _, err := s.cat.GetManifest(ctx, manifest.Slug); err != nil {
		return Installation{}, err
	}
	if existing, err := s.ins.Get(ctx, tenantID, manifest.Slug); err == nil && existing.State != StateUninstalled {
		return Installation{}, fmt.Errorf("%w: tenant=%s slug=%s", ErrPluginAlreadyInstalled, tenantID, manifest.Slug)
	}
	now := s.clock()
	row := Installation{
		TenantID:         tenantID,
		Slug:             manifest.Slug,
		InstalledVersion: manifest.Version,
		State:            StateInstalled,
		InstalledAt:      now,
		UpdatedAt:        now,
	}
	if err := s.ins.Create(ctx, row); err != nil {
		return Installation{}, err
	}
	if err := s.subs.Replace(ctx, tenantID, manifest.Slug, manifest.EventSubscriptions); err != nil {
		return Installation{}, err
	}
	return row, nil
}

// Activate transitions the installation to StateActive.
func (s *Service) Activate(ctx context.Context, tenantID, slug string) (Installation, error) {
	return s.transition(ctx, tenantID, slug, TransitionActivate)
}

// Deactivate transitions the installation to StateDeactivated.
func (s *Service) Deactivate(ctx context.Context, tenantID, slug string) (Installation, error) {
	return s.transition(ctx, tenantID, slug, TransitionDeactivate)
}

// Uninstall transitions the installation to StateUninstalled and
// deletes the row + subscriptions. Idempotent for already-uninstalled
// rows (returns nil without an error so the caller can treat
// uninstall as fire-and-forget).
func (s *Service) Uninstall(ctx context.Context, tenantID, slug string) error {
	if err := requireTenantSlug(tenantID, slug); err != nil {
		return err
	}
	row, err := s.ins.Get(ctx, tenantID, slug)
	if err != nil {
		return err
	}
	if _, err := nextState(row.State, TransitionUninstall); err != nil {
		return err
	}
	if err := s.subs.Delete(ctx, tenantID, slug); err != nil {
		return err
	}
	return s.ins.Delete(ctx, tenantID, slug)
}

// List returns the installations for a tenant.
func (s *Service) List(ctx context.Context, tenantID string, page, perPage int) ([]Installation, int, error) {
	if err := requireTenant(tenantID); err != nil {
		return nil, 0, err
	}
	return s.ins.List(ctx, tenantID, page, perPage)
}

// Get returns a single installation row.
func (s *Service) Get(ctx context.Context, tenantID, slug string) (Installation, error) {
	if err := requireTenantSlug(tenantID, slug); err != nil {
		return Installation{}, err
	}
	return s.ins.Get(ctx, tenantID, slug)
}

// transition runs the shared logic for activate/deactivate. Keeping
// the helper small (cyclomatic <10) makes the public methods one-liners.
func (s *Service) transition(ctx context.Context, tenantID, slug string, t Transition) (Installation, error) {
	if err := requireTenantSlug(tenantID, slug); err != nil {
		return Installation{}, err
	}
	row, err := s.ins.Get(ctx, tenantID, slug)
	if err != nil {
		return Installation{}, err
	}
	target, err := nextState(row.State, t)
	if err != nil {
		return Installation{}, err
	}
	row.State = target
	row.UpdatedAt = s.clock()
	if t == TransitionActivate && row.ActivatedAt == "" {
		row.ActivatedAt = row.UpdatedAt
	}
	if err := s.sb.RecordHook(tenantID, slug, string(t)); err != nil {
		return Installation{}, err
	}
	if err := s.ins.SaveState(ctx, row); err != nil {
		return Installation{}, err
	}
	return row, nil
}

// requireTenant returns ErrManifestInvalid (using ManifestInvalid as
// the "input invalid" sentinel keeps the error surface small) when
// the tenant id is empty.
func requireTenant(tenantID string) error {
	if strings.TrimSpace(tenantID) == "" {
		return fmt.Errorf("%w: tenant id required", ErrManifestInvalid)
	}
	return nil
}

func requireTenantSlug(tenantID, slug string) error {
	if err := requireTenant(tenantID); err != nil {
		return err
	}
	if !IsValidSlug(slug) {
		return fmt.Errorf("%w: slug=%q", ErrSlugInvalid, slug)
	}
	return nil
}

// noopSubscriptionRepo is the fallback when callers do not wire a
// subscription repository (e.g. unit tests that don't care).
type noopSubscriptionRepo struct{}

func (noopSubscriptionRepo) Replace(_ context.Context, _, _ string, _ []EventName) error {
	return nil
}
func (noopSubscriptionRepo) List(_ context.Context, _, _ string) ([]EventName, error) {
	return nil, nil
}
func (noopSubscriptionRepo) Delete(_ context.Context, _, _ string) error { return nil }
