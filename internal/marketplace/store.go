// File scope: v4.8.0 Story 3 -- vendor store interface (port).
//
// VendorStore is the repository port for vendor CRUD operations.
// Production: Postgres + RLS. Tests: in-memory implementation below.
package marketplace

import (
	"context"
	"sync"
)

type VendorStore interface {
	Create(ctx context.Context, vendor Vendor) error
	Get(ctx context.Context, tenantID, vendorID string) (Vendor, error)
	List(ctx context.Context, tenantID string) ([]Vendor, error)
	Update(ctx context.Context, vendor Vendor) error
	Deactivate(ctx context.Context, tenantID, vendorID string) error
}

type InMemoryVendorStore struct {
	mu      sync.Mutex
	vendors map[string]map[string]Vendor // tenantID -> vendorID -> Vendor
}

func NewInMemoryVendorStore() *InMemoryVendorStore {
	return &InMemoryVendorStore{
		vendors: make(map[string]map[string]Vendor),
	}
}

func (s *InMemoryVendorStore) Create(_ context.Context, v Vendor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vendors[v.TenantID] == nil {
		s.vendors[v.TenantID] = make(map[string]Vendor)
	}
	s.vendors[v.TenantID][v.VendorID] = v
	return nil
}

func (s *InMemoryVendorStore) Get(_ context.Context, tenantID, vendorID string) (Vendor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantVendors, ok := s.vendors[tenantID]
	if !ok {
		return Vendor{}, ErrVendorNotFound
	}
	v, ok := tenantVendors[vendorID]
	if !ok {
		return Vendor{}, ErrVendorNotFound
	}
	return v, nil
}

func (s *InMemoryVendorStore) List(_ context.Context, tenantID string) ([]Vendor, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tenantVendors := s.vendors[tenantID]
	result := make([]Vendor, 0, len(tenantVendors))
	for _, v := range tenantVendors {
		result = append(result, v)
	}
	return result, nil
}

func (s *InMemoryVendorStore) Update(_ context.Context, v Vendor) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vendors[v.TenantID] == nil {
		return ErrVendorNotFound
	}
	if _, ok := s.vendors[v.TenantID][v.VendorID]; !ok {
		return ErrVendorNotFound
	}
	s.vendors[v.TenantID][v.VendorID] = v
	return nil
}

func (s *InMemoryVendorStore) Deactivate(_ context.Context, tenantID, vendorID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.vendors[tenantID] == nil {
		return ErrVendorNotFound
	}
	v, ok := s.vendors[tenantID][vendorID]
	if !ok {
		return ErrVendorNotFound
	}
	v.Status = VendorStatusDeactivated
	s.vendors[tenantID][vendorID] = v
	return nil
}
