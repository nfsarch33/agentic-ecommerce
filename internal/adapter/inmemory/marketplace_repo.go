// Package inmemory carries the in-memory adapter implementations used
// by tests and dev mode. The marketplace adapters mirror the shape of
// the digital adapters in this same package.
package inmemory

import (
	"context"
	"fmt"
	"sort"
	"sync"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

// MarketplaceCatalog is the in-memory implementation of
// marketplace.CatalogRepository.
type MarketplaceCatalog struct {
	mu     sync.RWMutex
	bySlug map[string]marketplace.Manifest
}

// NewMarketplaceCatalog returns an empty catalogue.
func NewMarketplaceCatalog() *MarketplaceCatalog {
	return &MarketplaceCatalog{bySlug: make(map[string]marketplace.Manifest)}
}

// RegisterManifest stores a manifest. ErrSlugAlreadyExists when the
// slug is taken.
func (c *MarketplaceCatalog) RegisterManifest(_ context.Context, m marketplace.Manifest) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.bySlug[m.Slug]; ok {
		return fmt.Errorf("%w: %s", marketplace.ErrSlugAlreadyExists, m.Slug)
	}
	c.bySlug[m.Slug] = m
	return nil
}

// GetManifest returns the manifest for slug.
func (c *MarketplaceCatalog) GetManifest(_ context.Context, slug string) (marketplace.Manifest, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	m, ok := c.bySlug[slug]
	if !ok {
		return marketplace.Manifest{}, fmt.Errorf("%w: slug=%s", marketplace.ErrPluginNotFound, slug)
	}
	return m, nil
}

// ListManifests returns paginated manifests sorted by slug.
func (c *MarketplaceCatalog) ListManifests(_ context.Context, page, perPage int) ([]marketplace.Manifest, int, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	manifests := make([]marketplace.Manifest, 0, len(c.bySlug))
	for _, m := range c.bySlug {
		manifests = append(manifests, m)
	}
	sort.Slice(manifests, func(i, j int) bool { return manifests[i].Slug < manifests[j].Slug })
	total := len(manifests)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	out := make([]marketplace.Manifest, end-start)
	copy(out, manifests[start:end])
	return out, total, nil
}

// MarketplaceInstallations is the in-memory implementation of
// marketplace.InstallationRepository.
type MarketplaceInstallations struct {
	mu       sync.RWMutex
	byTenant map[string]map[string]marketplace.Installation
}

// NewMarketplaceInstallations builds an empty store.
func NewMarketplaceInstallations() *MarketplaceInstallations {
	return &MarketplaceInstallations{byTenant: make(map[string]map[string]marketplace.Installation)}
}

// Create inserts a new installation row.
func (r *MarketplaceInstallations) Create(_ context.Context, ins marketplace.Installation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[ins.TenantID] == nil {
		r.byTenant[ins.TenantID] = make(map[string]marketplace.Installation)
	}
	if _, ok := r.byTenant[ins.TenantID][ins.Slug]; ok {
		return fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginAlreadyInstalled, ins.TenantID, ins.Slug)
	}
	r.byTenant[ins.TenantID][ins.Slug] = ins
	return nil
}

// Get returns the row for (tenant, slug).
func (r *MarketplaceInstallations) Get(_ context.Context, tenantID, slug string) (marketplace.Installation, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, ok := r.byTenant[tenantID]
	if !ok {
		return marketplace.Installation{}, fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginNotFound, tenantID, slug)
	}
	row, ok := rows[slug]
	if !ok {
		return marketplace.Installation{}, fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginNotFound, tenantID, slug)
	}
	return row, nil
}

// List returns paginated rows sorted by InstalledAt asc.
func (r *MarketplaceInstallations) List(_ context.Context, tenantID string, page, perPage int) ([]marketplace.Installation, int, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	rows := r.byTenant[tenantID]
	out := make([]marketplace.Installation, 0, len(rows))
	for _, row := range rows {
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].InstalledAt < out[j].InstalledAt })
	total := len(out)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	page1 := make([]marketplace.Installation, end-start)
	copy(page1, out[start:end])
	return page1, total, nil
}

// SaveState persists an updated row. Errors when the row is missing.
func (r *MarketplaceInstallations) SaveState(_ context.Context, ins marketplace.Installation) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, ok := r.byTenant[ins.TenantID]
	if !ok {
		return fmt.Errorf("%w: tenant=%s", marketplace.ErrPluginNotFound, ins.TenantID)
	}
	if _, ok := rows[ins.Slug]; !ok {
		return fmt.Errorf("%w: slug=%s", marketplace.ErrPluginNotFound, ins.Slug)
	}
	rows[ins.Slug] = ins
	return nil
}

// Delete removes the row.
func (r *MarketplaceInstallations) Delete(_ context.Context, tenantID, slug string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, ok := r.byTenant[tenantID]
	if !ok {
		return fmt.Errorf("%w: tenant=%s", marketplace.ErrPluginNotFound, tenantID)
	}
	if _, ok := rows[slug]; !ok {
		return fmt.Errorf("%w: slug=%s", marketplace.ErrPluginNotFound, slug)
	}
	delete(rows, slug)
	return nil
}

// MarketplaceSubscriptions is the in-memory implementation of
// marketplace.SubscriptionRepository.
type MarketplaceSubscriptions struct {
	mu       sync.RWMutex
	byTenant map[string]map[string][]marketplace.EventName
}

// NewMarketplaceSubscriptions builds an empty store.
func NewMarketplaceSubscriptions() *MarketplaceSubscriptions {
	return &MarketplaceSubscriptions{byTenant: make(map[string]map[string][]marketplace.EventName)}
}

// Replace overwrites the subscription set for (tenant, slug).
func (r *MarketplaceSubscriptions) Replace(_ context.Context, tenantID, slug string, events []marketplace.EventName) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.byTenant[tenantID] == nil {
		r.byTenant[tenantID] = make(map[string][]marketplace.EventName)
	}
	copyEvents := make([]marketplace.EventName, len(events))
	copy(copyEvents, events)
	r.byTenant[tenantID][slug] = copyEvents
	return nil
}

// List returns the subscription set for (tenant, slug).
func (r *MarketplaceSubscriptions) List(_ context.Context, tenantID, slug string) ([]marketplace.EventName, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, ok := r.byTenant[tenantID]
	if !ok {
		return nil, nil
	}
	events := rows[slug]
	out := make([]marketplace.EventName, len(events))
	copy(out, events)
	return out, nil
}

// Delete removes the subscription row.
func (r *MarketplaceSubscriptions) Delete(_ context.Context, tenantID, slug string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, ok := r.byTenant[tenantID]
	if !ok {
		return nil
	}
	delete(rows, slug)
	return nil
}
