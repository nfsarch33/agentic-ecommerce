package sdk

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/marketplace"
)

// TestSandbox is a deterministic, in-memory marketplace runtime that
// plugin authors use in their unit tests. It wires the host's real
// Registry against in-memory adapters so a third-party plugin can
// drive the full Install/Activate/Deactivate/Uninstall cycle without
// standing up Postgres, Redis, or HTTP listeners.
//
// TestSandbox is goroutine-safe.
type TestSandbox struct {
	mu       sync.Mutex
	tb       testing.TB
	registry *marketplace.Service
	tenant   string
	manifest Manifest
}

// SandboxOption tunes optional knobs of the test sandbox.
type SandboxOption func(*sandboxOptions)

type sandboxOptions struct {
	tenant string
	clock  marketplace.Clock
}

// WithTenant overrides the default tenant id ("tenant-test"). Useful
// when the plugin under test cares about a specific id format.
func WithTenant(id string) SandboxOption {
	return func(o *sandboxOptions) { o.tenant = id }
}

// WithClock injects a deterministic timestamp source. Defaults to a
// fixed instant so test output stays stable.
func WithClock(c marketplace.Clock) SandboxOption {
	return func(o *sandboxOptions) { o.clock = c }
}

// NewTestSandbox constructs an isolated, deterministic sandbox the
// plugin author can use to drive lifecycle hooks. The sandbox calls
// t.Helper() and registers a t.Cleanup hook so failures point at the
// caller and resources are released after the test exits.
//
// Passing a nil manifest is treated as a fatal test failure: the
// SDK refuses to host a plugin with no identity.
func NewTestSandbox(tb testing.TB, manifest Manifest, opts ...SandboxOption) *TestSandbox {
	tb.Helper()
	options := sandboxOptions{
		tenant: "tenant-test",
		clock:  func() string { return "2026-05-09T00:00:00Z" },
	}
	for _, opt := range opts {
		opt(&options)
	}
	if err := manifest.Validate(); err != nil {
		tb.Fatalf("sdk.NewTestSandbox: invalid manifest: %v", err)
	}
	cat := inmemory.NewMarketplaceCatalog()
	if err := cat.RegisterManifest(context.Background(), manifest); err != nil {
		tb.Fatalf("sdk.NewTestSandbox: register manifest: %v", err)
	}
	svc, err := marketplace.NewService(marketplace.ServiceConfig{
		Catalog:       cat,
		Installations: inmemory.NewMarketplaceInstallations(),
		Subscriptions: inmemory.NewMarketplaceSubscriptions(),
		Sandbox:       marketplace.NewPermissiveSandbox(),
		Clock:         options.clock,
	})
	if err != nil {
		tb.Fatalf("sdk.NewTestSandbox: build registry: %v", err)
	}
	sb := &TestSandbox{
		tb:       tb,
		registry: svc,
		tenant:   options.tenant,
		manifest: manifest,
	}
	tb.Cleanup(sb.cleanup)
	return sb
}

// TenantID returns the tenant id used for sandbox operations.
func (s *TestSandbox) TenantID() string { return s.tenant }

// Manifest returns the manifest the sandbox was constructed with.
func (s *TestSandbox) Manifest() Manifest { return s.manifest }

// Install drives the plugin through the registry's Install path.
// Returns the Installation row so the test can assert on state and
// timestamps.
func (s *TestSandbox) Install(ctx context.Context, plugin Plugin) (Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if plugin == nil {
		return Installation{}, errors.New("sdk.TestSandbox.Install: plugin is nil")
	}
	row, err := s.registry.Install(ctx, s.tenant, plugin.Manifest())
	if err != nil {
		return Installation{}, fmt.Errorf("install: %w", err)
	}
	if err := plugin.Install(ctx, s.tenant); err != nil {
		return Installation{}, fmt.Errorf("plugin install hook: %w", err)
	}
	return row, nil
}

// Activate drives the plugin through Registry.Activate plus the
// plugin's own Activate hook.
func (s *TestSandbox) Activate(ctx context.Context, plugin Plugin) (Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.registry.Activate(ctx, s.tenant, plugin.Manifest().Slug)
	if err != nil {
		return Installation{}, fmt.Errorf("activate: %w", err)
	}
	if err := plugin.Activate(ctx, s.tenant); err != nil {
		return Installation{}, fmt.Errorf("plugin activate hook: %w", err)
	}
	return row, nil
}

// Deactivate drives the plugin through Registry.Deactivate plus the
// plugin's own Deactivate hook.
func (s *TestSandbox) Deactivate(ctx context.Context, plugin Plugin) (Installation, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	row, err := s.registry.Deactivate(ctx, s.tenant, plugin.Manifest().Slug)
	if err != nil {
		return Installation{}, fmt.Errorf("deactivate: %w", err)
	}
	if err := plugin.Deactivate(ctx, s.tenant); err != nil {
		return Installation{}, fmt.Errorf("plugin deactivate hook: %w", err)
	}
	return row, nil
}

// Uninstall drives the plugin through Registry.Uninstall and runs the
// plugin's own Uninstall hook before the registry tears the row down.
func (s *TestSandbox) Uninstall(ctx context.Context, plugin Plugin) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := plugin.Uninstall(ctx, s.tenant); err != nil {
		return fmt.Errorf("plugin uninstall hook: %w", err)
	}
	if err := s.registry.Uninstall(ctx, s.tenant, plugin.Manifest().Slug); err != nil {
		return fmt.Errorf("uninstall: %w", err)
	}
	return nil
}

// SmokeCheck runs the full lifecycle (Install -> Activate ->
// Deactivate -> Uninstall) against the supplied plugin and reports
// failures via the test framework. Plugin authors call this from a
// dedicated TestPluginSmoke test to gate every PR.
func (s *TestSandbox) SmokeCheck(ctx context.Context, plugin Plugin) {
	s.tb.Helper()
	if _, err := s.Install(ctx, plugin); err != nil {
		s.tb.Fatalf("smoke install: %v", err)
	}
	if _, err := s.Activate(ctx, plugin); err != nil {
		s.tb.Fatalf("smoke activate: %v", err)
	}
	if _, err := s.Deactivate(ctx, plugin); err != nil {
		s.tb.Fatalf("smoke deactivate: %v", err)
	}
	if err := s.Uninstall(ctx, plugin); err != nil {
		s.tb.Fatalf("smoke uninstall: %v", err)
	}
}

// Settings returns the per-tenant settings map (defensive copy).
func (s *TestSandbox) Settings() map[string]any {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.registry.Settings(s.tenant, s.manifest.Slug)
}

// SetSettings overwrites the per-tenant settings map.
func (s *TestSandbox) SetSettings(values map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry.SetSettings(s.tenant, s.manifest.Slug, values)
}

// HookTimeout returns the sandbox's hook deadline so plugin authors
// can wrap their hook calls in matching context.WithTimeout values.
func (s *TestSandbox) HookTimeout() time.Duration {
	return s.registry.Sandbox().HookTimeout()
}

// HooksRecorded returns the global hook count for telemetry assertions.
func (s *TestSandbox) HooksRecorded() uint64 {
	return s.registry.Sandbox().HooksRecorded()
}

func (s *TestSandbox) cleanup() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.registry = nil
}
