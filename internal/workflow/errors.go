// Package workflow sentinel errors.
//
// v4.1.1 IC-3: typed sentinel errors with %w wrapping so callers can
// use errors.Is/errors.As across package boundaries.
package workflow

import "errors"

var (
	ErrLauncherUnconfigured = errors.New("onboarding wizard launcher unconfigured")
	ErrWizardIdentityNil    = errors.New("wizard identity required for launch")

	ErrOrderTenantRequired    = errors.New("order_aggregator: tenant_id required")
	ErrOrderChannelRequired   = errors.New("order_aggregator: channel required")
	ErrOrderExternalIDMissing = errors.New("order_aggregator: external_order_id required")
	ErrOrderNoLineItems       = errors.New("order_aggregator: at least one line item required")
	ErrOrderOccurredAtMissing = errors.New("order_aggregator: occurred_at required")
)
