package inmemory

import (
	"context"
	"errors"
	"sort"
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
	mu       sync.RWMutex
	products map[uuid.UUID]catalog.Product
	slugIdx  map[string]uuid.UUID
}

func NewProductRepository() *ProductRepository {
	return &ProductRepository{
		products: make(map[uuid.UUID]catalog.Product),
		slugIdx:  make(map[string]uuid.UUID),
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
