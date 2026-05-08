package marketplace

import (
	"context"
)

// Plugin is the lifecycle interface every marketplace plugin
// implementation satisfies. Hooks are idempotent: calling Activate
// twice is a no-op once the underlying installation row is already
// active. Implementations MUST honour ctx cancellation.
//
// Optional seams (Subscribe, Routes) keep the interface narrow for
// plugins that only need lifecycle coverage. Type-assert to the
// optional interfaces when wiring richer behaviour.
type Plugin interface {
	// Manifest returns the immutable manifest the plugin ships with.
	// Used by the registry on first install to persist the row.
	Manifest() Manifest

	// Install runs once per (tenant, plugin) pair. Adapters call this
	// inside the Registry.Install state transition.
	Install(ctx context.Context, tenantID string) error

	// Activate transitions the installation from installed/deactivated
	// to active. Idempotent.
	Activate(ctx context.Context, tenantID string) error

	// Deactivate transitions the installation from active to
	// deactivated. Idempotent.
	Deactivate(ctx context.Context, tenantID string) error

	// Uninstall is the terminal hook. Adapters delete the row after
	// this returns.
	Uninstall(ctx context.Context, tenantID string) error
}

// EventSubscriber is implemented by plugins that consume events.
// Returned slice MUST be a subset of the manifest's
// EventSubscriptions or the registry rejects activation.
type EventSubscriber interface {
	Subscribe(events ...EventName) []EventName
}

// RouteExtender is implemented by plugins that mount additional HTTP
// routes. Routes are URL-prefixed by the registry to namespace by
// tenant: e.g. /api/v1/marketplace/tenants/<tenant>/plugins/<slug>/<path>.
type RouteExtender interface {
	Routes(prefix string) []Route
}

// Route is a single route descriptor. The handler is plain
// http.Handler-compatible via the standard library; we keep it as a
// typed callback to avoid a hard import on net/http inside the
// domain package.
type Route struct {
	Method string
	Path   string
	// Permissions required to invoke the route, evaluated by the
	// host's RBAC layer before delegation.
	Permissions []Permission
}

// EventNames returns a defensive copy of the manifest's subscriptions
// for downstream wiring.
func EventNames(m Manifest) []EventName {
	out := make([]EventName, len(m.EventSubscriptions))
	copy(out, m.EventSubscriptions)
	return out
}

// Installation is the durable per-tenant per-plugin row persisted by
// the registry adapters.
type Installation struct {
	TenantID         string
	Slug             string
	InstalledVersion string
	State            State
	InstalledAt      string // RFC3339
	ActivatedAt      string // RFC3339, empty until first activate
	UpdatedAt        string // RFC3339
}

// IsActive reports whether the installation is currently active.
func (i Installation) IsActive() bool { return i.State == StateActive }
