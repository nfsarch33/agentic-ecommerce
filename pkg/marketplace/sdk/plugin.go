// Package sdk is the public Plugin SDK for the Agentic Ecommerce
// marketplace. It re-exports the safe lifecycle surface from
// internal/marketplace so third-party plugin authors can build against
// a stable, narrow API without depending on internal packages.
//
// Stability: every symbol exported here is governed by the v1 API
// stability policy documented in docs/api-versioning.md. Breaking
// changes require a major version bump of the host module.
//
// Quick start:
//
//	import (
//		"context"
//
//		"github.com/nfsarch33/agentic-ecommerce/pkg/marketplace/sdk"
//	)
//
//	type HelloPlugin struct{}
//
//	func (HelloPlugin) Manifest() sdk.Manifest {
//		return sdk.Manifest{
//			Slug:    "hello",
//			Name:    "Hello Plugin",
//			Version: "1.0.0",
//			Vendor:  "Example",
//		}
//	}
//	func (HelloPlugin) Install(ctx context.Context, tenantID string) error    { return nil }
//	func (HelloPlugin) Activate(ctx context.Context, tenantID string) error   { return nil }
//	func (HelloPlugin) Deactivate(ctx context.Context, tenantID string) error { return nil }
//	func (HelloPlugin) Uninstall(ctx context.Context, tenantID string) error  { return nil }
package sdk

import (
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

// Plugin is the lifecycle interface every marketplace plugin
// implementation satisfies. Hooks are idempotent: calling Activate
// twice is a no-op once the underlying installation row is already
// active. Implementations MUST honour ctx cancellation.
//
// This is a type alias for the canonical interface in
// internal/marketplace; the alias makes the SDK package the single
// public import path for plugin authors while keeping the runtime
// contract centralised.
type Plugin = marketplace.Plugin

// EventSubscriber is implemented by plugins that consume events from
// the host event bus. The returned slice MUST be a subset of the
// manifest's EventSubscriptions or the registry rejects activation.
type EventSubscriber = marketplace.EventSubscriber

// RouteExtender is implemented by plugins that mount additional HTTP
// routes. Routes are URL-prefixed by the registry to namespace by
// tenant.
type RouteExtender = marketplace.RouteExtender

// Route is a single route descriptor exposed by a plugin.
type Route = marketplace.Route

// Manifest is the typed plugin manifest the registry persists. JSON
// tags match the on-the-wire shape used by /api/v1/marketplace/plugins
// so a plugin's manifest_test.go can assert the same JSON your tenants
// will see.
type Manifest = marketplace.Manifest

// EventName is a typed alias for an event-bus identifier the manifest
// may subscribe to.
type EventName = marketplace.EventName

// Permission is a coarse capability flag the plugin requests at
// install time.
type Permission = marketplace.Permission

// DependencyRef is a (slug, semverConstraint) pair.
type DependencyRef = marketplace.DependencyRef

// State is the lifecycle state of an Installation row.
type State = marketplace.State

// Installation is the durable per-tenant per-plugin row persisted by
// the registry adapters.
type Installation = marketplace.Installation

// Permission constants -- subset of the host's permission catalogue.
// SDK consumers reference these instead of stringly-typed values to
// pick up future expansion automatically.
const (
	PermissionReadCatalog     = marketplace.PermissionReadCatalog
	PermissionReadOrders      = marketplace.PermissionReadOrders
	PermissionWriteOrders     = marketplace.PermissionWriteOrders
	PermissionReadMemberships = marketplace.PermissionReadMemberships
	PermissionReadDigital     = marketplace.PermissionReadDigital
	PermissionEmitEvents      = marketplace.PermissionEmitEvents
)

// Lifecycle state constants.
const (
	StateInstalled   = marketplace.StateInstalled
	StateActive      = marketplace.StateActive
	StateDeactivated = marketplace.StateDeactivated
	StateUninstalled = marketplace.StateUninstalled
)

// Re-export the typed sentinel errors so plugin authors can use
// errors.Is for assertions in their own tests.
var (
	ErrManifestInvalid        = marketplace.ErrManifestInvalid
	ErrSlugInvalid            = marketplace.ErrSlugInvalid
	ErrSemverInvalid          = marketplace.ErrSemverInvalid
	ErrPluginAlreadyInstalled = marketplace.ErrPluginAlreadyInstalled
	ErrPluginNotFound         = marketplace.ErrPluginNotFound
	ErrInvalidTransition      = marketplace.ErrInvalidTransition
	ErrSandboxBudgetExceeded  = marketplace.ErrSandboxBudgetExceeded
	ErrCrossTenantAccess      = marketplace.ErrCrossTenantAccess
)

// IsValidSlug reports whether s matches the kebab-case rule used for
// plugin and tenant slugs.
func IsValidSlug(s string) bool { return marketplace.IsValidSlug(s) }

// IsValidSemver reports whether s parses as MAJOR.MINOR.PATCH.
func IsValidSemver(s string) bool { return marketplace.IsValidSemver(s) }

// EventNames returns a defensive copy of the manifest's event
// subscriptions.
func EventNames(m Manifest) []EventName { return marketplace.EventNames(m) }
