package tenant

import (
	"errors"
	"sync"
)

var (
	ErrTenantNotFound   = errors.New("tenant not found")
	ErrDuplicateSlug    = errors.New("slug already taken")
	ErrCrossTenantAccess = errors.New("cross-tenant access denied")
)

type Tenant struct {
	ID   string
	Name string
	Slug string
}

type IsolatedRecord struct {
	TenantID string
	Data     any
}

type Isolator struct {
	mu      sync.RWMutex
	tenants map[string]*Tenant // slug -> tenant
	byID    map[string]*Tenant // id -> tenant
}

func NewIsolator() *Isolator {
	return &Isolator{
		tenants: make(map[string]*Tenant),
		byID:    make(map[string]*Tenant),
	}
}

func (i *Isolator) CreateTenant(id, name, slug string) (Tenant, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	if _, exists := i.tenants[slug]; exists {
		return Tenant{}, ErrDuplicateSlug
	}
	t := &Tenant{ID: id, Name: name, Slug: slug}
	i.tenants[slug] = t
	i.byID[id] = t
	return *t, nil
}

func (i *Isolator) ResolveTenant(slug string) (Tenant, error) {
	i.mu.RLock()
	defer i.mu.RUnlock()
	t, ok := i.tenants[slug]
	if !ok {
		return Tenant{}, ErrTenantNotFound
	}
	return *t, nil
}

func (i *Isolator) IsolateData(tenantID string, data any) (IsolatedRecord, error) {
	i.mu.RLock()
	_, ok := i.byID[tenantID]
	i.mu.RUnlock()
	if !ok {
		return IsolatedRecord{}, ErrTenantNotFound
	}
	return IsolatedRecord{TenantID: tenantID, Data: data}, nil
}

func (i *Isolator) CrossTenantGuard(tenantID, requestTenantID string) error {
	if tenantID != requestTenantID {
		return ErrCrossTenantAccess
	}
	return nil
}
