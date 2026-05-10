// Package residency implements per-tenant data residency enforcement.
// v4.9.0 Story 1: tenants declare a home region (AU/CN/EU/US) and
// the Router + Validator ensure data writes respect that declaration.
//
// Decomposition (HARD GATE: complex_fn=4):
//   - NewPolicy          -> construct (cyclomatic 2)
//   - Validate           -> check region match (cyclomatic 3)
//   - Router.RouteWrite  -> select pool (cyclomatic 3)
//   - Router.RouteRead   -> select pool (cyclomatic 2)
//   - DefaultRegion      -> constant (cyclomatic 1)
package residency

import (
	"context"
	"errors"
	"fmt"
)

// RegionCode enumerates the supported data residency regions.
type RegionCode string

const (
	RegionAU RegionCode = "AU"
	RegionCN RegionCode = "CN"
	RegionEU RegionCode = "EU"
	RegionUS RegionCode = "US"
)

// DefaultRegion is used when a tenant has no explicit data_region.
func DefaultRegion() RegionCode { return RegionAU }

// ValidRegions returns the set of recognised region codes.
func ValidRegions() []RegionCode {
	return []RegionCode{RegionAU, RegionCN, RegionEU, RegionUS}
}

// IsValid returns true if the region code is recognised.
func (r RegionCode) IsValid() bool {
	for _, v := range ValidRegions() {
		if v == r {
			return true
		}
	}
	return false
}

// GCPZone returns the GCP zone annotation for this region.
func (r RegionCode) GCPZone() string {
	switch r {
	case RegionAU:
		return "australia-southeast1"
	case RegionCN:
		return "asia-east2"
	case RegionEU:
		return "europe-west1"
	case RegionUS:
		return "us-central1"
	default:
		return "australia-southeast1"
	}
}

var (
	ErrResidencyViolation = errors.New("residency: data would be written to wrong region")
	ErrUnknownRegion      = errors.New("residency: unknown region code")
	ErrTenantNotFound     = errors.New("residency: tenant not found")
)

// Policy defines where a tenant's data must reside.
type Policy struct {
	TenantID   string
	DataRegion RegionCode
}

// NewPolicy constructs a validated residency policy.
func NewPolicy(tenantID string, region RegionCode) (Policy, error) {
	if tenantID == "" {
		return Policy{}, fmt.Errorf("residency: tenant_id required")
	}
	if !region.IsValid() {
		return Policy{}, fmt.Errorf("%w: %s", ErrUnknownRegion, region)
	}
	return Policy{TenantID: tenantID, DataRegion: region}, nil
}

// TenantRegionResolver looks up a tenant's declared region.
type TenantRegionResolver interface {
	TenantRegion(ctx context.Context, tenantID string) (RegionCode, error)
}

// Validator is middleware that rejects writes violating residency.
type Validator struct {
	resolver TenantRegionResolver
}

// NewValidator constructs a Validator.
func NewValidator(resolver TenantRegionResolver) *Validator {
	return &Validator{resolver: resolver}
}

// Validate checks that a write targeting targetRegion is permitted
// for the given tenant.
func (v *Validator) Validate(ctx context.Context, tenantID string, targetRegion RegionCode) error {
	declared, err := v.resolver.TenantRegion(ctx, tenantID)
	if err != nil {
		return fmt.Errorf("residency: resolve tenant %s: %w", tenantID, err)
	}
	if declared != targetRegion {
		return fmt.Errorf("%w: tenant %s declared %s, target %s",
			ErrResidencyViolation, tenantID, declared, targetRegion)
	}
	return nil
}

// ConnectionPool is the interface for a region-specific DB pool.
type ConnectionPool interface {
	Region() RegionCode
}

// Router routes database operations to region-specific pools.
// Current implementation uses a single pool with region annotation;
// future multi-region Postgres will use distinct pools per region.
type Router struct {
	pools    map[RegionCode]ConnectionPool
	default_ ConnectionPool
}

// NewRouter constructs a Router from a pool map.
func NewRouter(pools map[RegionCode]ConnectionPool, defaultPool ConnectionPool) *Router {
	return &Router{pools: pools, default_: defaultPool}
}

// RouteWrite selects the connection pool for a write operation.
func (r *Router) RouteWrite(region RegionCode) (ConnectionPool, error) {
	if pool, ok := r.pools[region]; ok {
		return pool, nil
	}
	if r.default_ != nil {
		return r.default_, nil
	}
	return nil, fmt.Errorf("%w: no pool for %s", ErrUnknownRegion, region)
}

// RouteRead selects the connection pool for a read operation.
func (r *Router) RouteRead(region RegionCode) (ConnectionPool, error) {
	return r.RouteWrite(region)
}

// ResidencyMetrics is the metrics port for residency violations.
type ResidencyMetrics interface {
	IncResidencyViolation(tenantID string, fromRegion, toRegion RegionCode)
}
