package inmemory

import (
	"context"
	"errors"
	"sort"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

var (
	ErrProductNotFound  = errors.New("product not found")
	ErrDuplicateProduct = errors.New("duplicate product")
)

type ProductRepository struct {
	mu             sync.RWMutex
	products       map[uuid.UUID]catalog.Product
	slugIdx        map[string]uuid.UUID
	tenantProducts map[string]map[uuid.UUID]struct{}
}

func NewProductRepository() *ProductRepository {
	return &ProductRepository{
		products:       make(map[uuid.UUID]catalog.Product),
		slugIdx:        make(map[string]uuid.UUID),
		tenantProducts: make(map[string]map[uuid.UUID]struct{}),
	}
}

func (r *ProductRepository) Create(_ context.Context, product catalog.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.products[product.ID()]; exists {
		return ErrDuplicateProduct
	}
	if _, exists := r.slugIdx[product.Slug()]; exists {
		return ErrDuplicateProduct
	}

	r.products[product.ID()] = product
	r.slugIdx[product.Slug()] = product.ID()
	return nil
}

func (r *ProductRepository) CreateWithTenant(_ context.Context, product catalog.Product, tenantID string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrProductNotFound
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if r.tenantProducts == nil {
		r.tenantProducts = make(map[string]map[uuid.UUID]struct{})
	}
	if existing, exists := r.products[product.ID()]; exists {
		if existing.Slug() != product.Slug() {
			return ErrDuplicateProduct
		}
		if r.tenantProducts[tenantID] == nil {
			r.tenantProducts[tenantID] = make(map[uuid.UUID]struct{})
		}
		r.tenantProducts[tenantID][product.ID()] = struct{}{}
		return nil
	}
	if existingID, exists := r.slugIdx[product.Slug()]; exists && existingID != product.ID() {
		return ErrDuplicateProduct
	}
	r.products[product.ID()] = product
	r.slugIdx[product.Slug()] = product.ID()
	if r.tenantProducts[tenantID] == nil {
		r.tenantProducts[tenantID] = make(map[uuid.UUID]struct{})
	}
	r.tenantProducts[tenantID][product.ID()] = struct{}{}
	return nil
}

func (r *ProductRepository) GetByID(_ context.Context, id uuid.UUID) (catalog.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.products[id]
	if !ok {
		return catalog.Product{}, ErrProductNotFound
	}
	return p, nil
}

func (r *ProductRepository) GetBySlug(_ context.Context, slug string) (catalog.Product, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	id, ok := r.slugIdx[slug]
	if !ok {
		return catalog.Product{}, ErrProductNotFound
	}
	return r.products[id], nil
}

func (r *ProductRepository) List(_ context.Context, page, perPage int) (port.ListResult, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	all := make([]catalog.Product, 0, len(r.products))
	for _, p := range r.products {
		all = append(all, p)
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt().Before(all[j].CreatedAt())
	})

	total := len(all)
	start := (page - 1) * perPage
	if start >= total {
		return port.ListResult{Total: total}, nil
	}
	end := start + perPage
	if end > total {
		end = total
	}

	return port.ListResult{
		Products: all[start:end],
		Total:    total,
	}, nil
}

func (r *ProductRepository) ListByTenant(_ context.Context, tenantID string, page, perPage int) (port.ListResult, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return port.ListResult{}, ErrProductNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()

	ids := r.tenantProducts[tenantID]
	all := make([]catalog.Product, 0, len(ids))
	for id := range ids {
		if product, ok := r.products[id]; ok {
			all = append(all, product)
		}
	}
	sort.Slice(all, func(i, j int) bool {
		return all[i].CreatedAt().Before(all[j].CreatedAt())
	})

	total := len(all)
	start := (page - 1) * perPage
	if start >= total {
		return port.ListResult{Total: total}, nil
	}
	end := start + perPage
	if end > total {
		end = total
	}
	return port.ListResult{Products: all[start:end], Total: total}, nil
}

func (r *ProductRepository) GetByIDAndTenant(_ context.Context, id uuid.UUID, tenantID string) (catalog.Product, error) {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return catalog.Product{}, ErrProductNotFound
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	if _, ok := r.tenantProducts[tenantID][id]; !ok {
		return catalog.Product{}, ErrProductNotFound
	}
	product, ok := r.products[id]
	if !ok {
		return catalog.Product{}, ErrProductNotFound
	}
	return product, nil
}

func (r *ProductRepository) Update(_ context.Context, product catalog.Product) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	old, ok := r.products[product.ID()]
	if !ok {
		return ErrProductNotFound
	}

	if old.Slug() != product.Slug() {
		delete(r.slugIdx, old.Slug())
		r.slugIdx[product.Slug()] = product.ID()
	}

	r.products[product.ID()] = product
	return nil
}

func (r *ProductRepository) Delete(_ context.Context, id uuid.UUID) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	p, ok := r.products[id]
	if !ok {
		return ErrProductNotFound
	}

	delete(r.slugIdx, p.Slug())
	delete(r.products, id)
	return nil
}
