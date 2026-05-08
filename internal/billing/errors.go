// Package billing models the v2.5.0 tenant billing bounded context:
// per-tenant Subscriptions, Invoices, and UsageRecords plus the
// Stripe webhook bridge that feeds their state.
//
// The Subscription state machine mirrors the explicit transition-table
// pattern used by internal/domain/membership/state.go,
// internal/domain/digital/state.go, and internal/marketplace/state.go.
//
// All errors are typed sentinels so callers can use errors.Is checks
// across adapter boundaries.
package billing

import "errors"

// ErrSubscriptionNotFound is returned when a Subscription cannot be
// resolved by id or by Stripe subscription id.
var ErrSubscriptionNotFound = errors.New("billing subscription not found")

// ErrInvoiceNotFound is returned when an Invoice cannot be resolved.
var ErrInvoiceNotFound = errors.New("billing invoice not found")

// ErrInvalidTransition is returned when a Subscription transition is
// not permitted by the state machine (e.g. resume a canceled
// subscription).
var ErrInvalidTransition = errors.New("invalid billing subscription transition")

// ErrInvalidTransitionName is returned when a Transition value is
// not part of the canonical set.
var ErrInvalidTransitionName = errors.New("invalid billing subscription transition name")

// ErrInvalidState is returned when a string cannot be parsed into a
// canonical Subscription State.
var ErrInvalidState = errors.New("invalid billing subscription state")

// ErrInvalidInvoiceStatus is returned when a string cannot be parsed
// into a canonical Invoice status.
var ErrInvalidInvoiceStatus = errors.New("invalid billing invoice status")

// ErrTenantRequired is returned by ports/services when the tenant_id
// argument is missing.
var ErrTenantRequired = errors.New("billing tenant id required")

// ErrPlanNotFound is returned by the PlanCatalog when the plan id is
// unknown.
var ErrPlanNotFound = errors.New("billing plan not found")

// ErrSubscriptionAlreadyExists is returned when Create is asked to
// insert a Subscription whose (tenant, id) already exists.
var ErrSubscriptionAlreadyExists = errors.New("billing subscription already exists")

// ErrInvoiceAlreadyExists is returned when Create is asked to insert
// an Invoice whose (tenant, id) already exists.
var ErrInvoiceAlreadyExists = errors.New("billing invoice already exists")
