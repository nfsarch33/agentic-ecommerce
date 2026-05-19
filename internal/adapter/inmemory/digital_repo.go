package inmemory

import (
	"context"
	"sort"
	"sync"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/digital"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

// DigitalProductRepository is an in-memory implementation of
// port.DigitalProductRepository for tests and dev mode.
type DigitalProductRepository struct {
	mu       sync.RWMutex
	byTenant map[string]map[uuid.UUID]digital.DigitalProductRecord
}

// NewDigitalProductRepository builds an empty repository.
func NewDigitalProductRepository() *DigitalProductRepository {
	return &DigitalProductRepository{
		byTenant: make(map[string]map[uuid.UUID]digital.DigitalProductRecord),
	}
}

func (r *DigitalProductRepository) Create(ctx context.Context, tenantID string, p digital.DigitalProduct) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[tenantID] == nil {
		r.byTenant[tenantID] = make(map[uuid.UUID]digital.DigitalProductRecord)
	}
	r.byTenant[tenantID][p.ID()] = digitalProductToRecord(p)
	return nil
}

func (r *DigitalProductRepository) Update(ctx context.Context, tenantID string, p digital.DigitalProduct) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[tenantID] == nil {
		return port.ErrDigitalProductNotFound
	}
	if _, ok := r.byTenant[tenantID][p.ID()]; !ok {
		return port.ErrDigitalProductNotFound
	}
	r.byTenant[tenantID][p.ID()] = digitalProductToRecord(p)
	return nil
}

func (r *DigitalProductRepository) Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.DigitalProduct, error) {
	if err := ctx.Err(); err != nil {
		return digital.DigitalProduct{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.byTenant[tenantID][id]
	if !ok {
		return digital.DigitalProduct{}, port.ErrDigitalProductNotFound
	}
	return digital.ReconstructDigitalProduct(rec), nil
}

func (r *DigitalProductRepository) List(ctx context.Context, tenantID string, page, perPage int) (port.DigitalProductList, error) {
	if err := ctx.Err(); err != nil {
		return port.DigitalProductList{}, err
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := r.byTenant[tenantID]
	products := make([]digital.DigitalProduct, 0, len(rows))
	for _, rec := range rows {
		products = append(products, digital.ReconstructDigitalProduct(rec))
	}
	sort.Slice(products, func(i, j int) bool {
		return products[i].CreatedAt().Before(products[j].CreatedAt())
	})
	total := len(products)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return port.DigitalProductList{Products: products[start:end], Total: total}, nil
}

func (r *DigitalProductRepository) Delete(ctx context.Context, tenantID string, id uuid.UUID) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[tenantID] == nil {
		return port.ErrDigitalProductNotFound
	}
	if _, ok := r.byTenant[tenantID][id]; !ok {
		return port.ErrDigitalProductNotFound
	}
	delete(r.byTenant[tenantID], id)
	return nil
}

func digitalProductToRecord(p digital.DigitalProduct) digital.DigitalProductRecord {
	return digital.DigitalProductRecord{
		ID:          p.ID(),
		TenantID:    p.TenantID(),
		SKU:         p.SKU(),
		Name:        p.Name(),
		Description: p.Description(),
		FilePath:    p.FilePath(),
		FileSize:    p.FileSize(),
		ContentType: p.ContentType(),
		Checksum:    p.Checksum(),
		Version:     p.Version(),
		CreatedAt:   p.CreatedAt(),
		UpdatedAt:   p.UpdatedAt(),
	}
}

// LicenseRepository is an in-memory implementation of
// port.LicenseRepository.
type LicenseRepository struct {
	mu       sync.RWMutex
	byTenant map[string]map[uuid.UUID]digital.LicenseRecord
}

// NewLicenseRepository builds an empty in-memory licence store.
func NewLicenseRepository() *LicenseRepository {
	return &LicenseRepository{
		byTenant: make(map[string]map[uuid.UUID]digital.LicenseRecord),
	}
}

func (r *LicenseRepository) Create(ctx context.Context, tenantID string, lic digital.License) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[tenantID] == nil {
		r.byTenant[tenantID] = make(map[uuid.UUID]digital.LicenseRecord)
	}
	r.byTenant[tenantID][lic.ID()] = licenseToRecord(lic)
	return nil
}

func (r *LicenseRepository) Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.License, error) {
	if err := ctx.Err(); err != nil {
		return digital.License{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.byTenant[tenantID][id]
	if !ok {
		return digital.License{}, port.ErrLicenseNotFound
	}
	return digital.ReconstructLicense(rec), nil
}

func (r *LicenseRepository) List(ctx context.Context, tenantID string, page, perPage int) (port.LicenseList, error) {
	if err := ctx.Err(); err != nil {
		return port.LicenseList{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return paginateLicenses(r.byTenant[tenantID], page, perPage), nil
}

func (r *LicenseRepository) ListByCustomer(ctx context.Context, tenantID string, customerID uuid.UUID, page, perPage int) (port.LicenseList, error) {
	if err := ctx.Err(); err != nil {
		return port.LicenseList{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	filtered := make(map[uuid.UUID]digital.LicenseRecord)
	for id, rec := range r.byTenant[tenantID] {
		if rec.CustomerID == customerID {
			filtered[id] = rec
		}
	}
	return paginateLicenses(filtered, page, perPage), nil
}

func (r *LicenseRepository) SaveState(ctx context.Context, tenantID string, lic digital.License) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, ok := r.byTenant[tenantID]
	if !ok {
		return port.ErrLicenseNotFound
	}
	if _, ok := rows[lic.ID()]; !ok {
		return port.ErrLicenseNotFound
	}
	rows[lic.ID()] = licenseToRecord(lic)
	return nil
}

func licenseToRecord(lic digital.License) digital.LicenseRecord {
	return digital.LicenseRecord{
		ID:             lic.ID(),
		TenantID:       lic.TenantID(),
		ProductID:      lic.ProductID(),
		CustomerID:     lic.CustomerID(),
		Key:            lic.Key(),
		State:          lic.State(),
		IssuedAt:       lic.IssuedAt(),
		ExpiresAt:      lic.ExpiresAt(),
		MaxActivations: lic.MaxActivations(),
		UpdatedAt:      lic.UpdatedAt(),
	}
}

func paginateLicenses(rows map[uuid.UUID]digital.LicenseRecord, page, perPage int) port.LicenseList {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	licenses := make([]digital.License, 0, len(rows))
	for _, rec := range rows {
		licenses = append(licenses, digital.ReconstructLicense(rec))
	}
	sort.Slice(licenses, func(i, j int) bool {
		return licenses[i].IssuedAt().Before(licenses[j].IssuedAt())
	})
	total := len(licenses)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return port.LicenseList{Licenses: licenses[start:end], Total: total}
}

// AccessGrantRepository is an in-memory implementation of
// port.AccessGrantRepository. Upsert is keyed by
// (tenant_id, customer_id, product_id).
type AccessGrantRepository struct {
	mu       sync.RWMutex
	byTenant map[string]map[string]digital.AccessGrantRecord
}

// NewAccessGrantRepository builds an empty grant store.
func NewAccessGrantRepository() *AccessGrantRepository {
	return &AccessGrantRepository{
		byTenant: make(map[string]map[string]digital.AccessGrantRecord),
	}
}

func grantKey(customerID, productID uuid.UUID) string {
	return customerID.String() + ":" + productID.String()
}

func (r *AccessGrantRepository) Upsert(ctx context.Context, tenantID string, grant digital.AccessGrant) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[tenantID] == nil {
		r.byTenant[tenantID] = make(map[string]digital.AccessGrantRecord)
	}
	r.byTenant[tenantID][grantKey(grant.CustomerID(), grant.ProductID())] = digital.AccessGrantRecord{
		ID:         grant.ID(),
		TenantID:   grant.TenantID(),
		CustomerID: grant.CustomerID(),
		ProductID:  grant.ProductID(),
		LicenseID:  grant.LicenseID(),
		GrantedAt:  grant.GrantedAt(),
		Source:     grant.Source(),
	}
	return nil
}

func (r *AccessGrantRepository) Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.AccessGrant, error) {
	if err := ctx.Err(); err != nil {
		return digital.AccessGrant{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	for _, rec := range r.byTenant[tenantID] {
		if rec.ID == id {
			return digital.ReconstructAccessGrant(rec), nil
		}
	}
	return digital.AccessGrant{}, port.ErrAccessGrantNotFound
}

func (r *AccessGrantRepository) ListByCustomer(ctx context.Context, tenantID string, customerID uuid.UUID, page, perPage int) (port.AccessGrantList, error) {
	if err := ctx.Err(); err != nil {
		return port.AccessGrantList{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	grants := make([]digital.AccessGrant, 0)
	for _, rec := range r.byTenant[tenantID] {
		if rec.CustomerID == customerID {
			grants = append(grants, digital.ReconstructAccessGrant(rec))
		}
	}
	sort.Slice(grants, func(i, j int) bool {
		return grants[i].GrantedAt().Before(grants[j].GrantedAt())
	})
	total := len(grants)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return port.AccessGrantList{Grants: grants[start:end], Total: total}, nil
}

func (r *AccessGrantRepository) GetByCustomerProduct(ctx context.Context, tenantID string, customerID, productID uuid.UUID) (digital.AccessGrant, error) {
	if err := ctx.Err(); err != nil {
		return digital.AccessGrant{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.byTenant[tenantID][grantKey(customerID, productID)]
	if !ok {
		return digital.AccessGrant{}, port.ErrAccessGrantNotFound
	}
	return digital.ReconstructAccessGrant(rec), nil
}
