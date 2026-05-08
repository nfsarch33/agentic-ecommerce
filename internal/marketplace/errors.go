// Package marketplace implements the v2.4.0 plugin framework: lifecycle
// hooks, manifest validation, semver dependency resolution, sandbox
// enforcement, and a tenant-aware Registry.
//
// Errors are typed sentinels so callers can use errors.Is checks across
// adapter boundaries.
package marketplace

import "errors"

// ErrPluginAlreadyInstalled is returned when Install is called for a
// (tenant, slug) pair that already has an installation row.
var ErrPluginAlreadyInstalled = errors.New("plugin already installed for tenant")

// ErrPluginNotFound is returned when a plugin slug cannot be resolved
// in the catalogue or when a per-tenant installation is missing.
var ErrPluginNotFound = errors.New("plugin not found")

// ErrInvalidTransition is returned when an installation transition is
// not permitted by the state machine (e.g. activate from uninstalled).
var ErrInvalidTransition = errors.New("invalid installation transition")

// ErrInvalidTransitionName is returned when a Transition value is not
// part of the canonical set.
var ErrInvalidTransitionName = errors.New("invalid installation transition name")

// ErrSemverConflict is returned when dependency resolution detects a
// version pin that the installed catalogue cannot satisfy.
var ErrSemverConflict = errors.New("semver conflict")

// ErrSemverInvalid is returned when a version string fails the strict
// semver MAJOR.MINOR.PATCH validation.
var ErrSemverInvalid = errors.New("invalid semver")

// ErrSandboxBudgetExceeded is returned when a plugin exhausts its
// per-tenant rate-limit budget for outbound calls or hook invocations.
var ErrSandboxBudgetExceeded = errors.New("sandbox budget exceeded")

// ErrSlugInvalid is returned when a manifest slug fails the
// kebab-case regex.
var ErrSlugInvalid = errors.New("invalid plugin slug")

// ErrSlugAlreadyExists is returned when registering a manifest whose
// slug already exists in the catalogue.
var ErrSlugAlreadyExists = errors.New("plugin slug already registered")

// ErrUnknownEvent is returned when a manifest subscribes to an event
// name that is not registered with the schema registry.
var ErrUnknownEvent = errors.New("unknown event subscription")

// ErrManifestInvalid is returned when a manifest fails any of the
// structural validations (missing name/version/vendor, etc.).
var ErrManifestInvalid = errors.New("invalid plugin manifest")

// ErrDependencyCycle is returned when topological ordering detects a
// cycle in the dependency graph.
var ErrDependencyCycle = errors.New("plugin dependency cycle")

// ErrCrossTenantAccess is returned when a sandbox-wrapped operation
// attempts to read or write data outside the plugin's tenant scope.
var ErrCrossTenantAccess = errors.New("cross-tenant access denied")
