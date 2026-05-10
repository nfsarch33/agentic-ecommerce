// Package coord sentinel errors.
//
// v4.1.1 IC-3: typed sentinel errors with %w wrapping.
package coord

import "errors"

var (
	ErrAgentNameRequired = errors.New("coord: agent_name required")
	ErrTenantIDRequired  = errors.New("coord: tenant_id required")
	ErrSKURequired       = errors.New("coord: sku required")
	ErrActionRequired    = errors.New("coord: action required")
)
