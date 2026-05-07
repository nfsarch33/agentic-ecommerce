package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

type ListResult struct {
	Products []catalog.Product
	Total    int
}

type ProductRepository interface {
	Create(ctx context.Context, product catalog.Product) error
	GetByID(ctx context.Context, id uuid.UUID) (catalog.Product, error)
	GetBySlug(ctx context.Context, slug string) (catalog.Product, error)
	List(ctx context.Context, page, perPage int) (ListResult, error)
	Update(ctx context.Context, product catalog.Product) error
	Delete(ctx context.Context, id uuid.UUID) error
}
