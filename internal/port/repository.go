package port

import (
	"context"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
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

type OrderRepository interface {
	Create(ctx context.Context, order orderdomain.Order) error
	GetByID(ctx context.Context, id uuid.UUID) (orderdomain.Order, error)
	UpdateStatus(ctx context.Context, id uuid.UUID, status orderdomain.Status) (orderdomain.Order, error)
}

type CartRepository interface {
	Save(ctx context.Context, cart orderdomain.Cart) error
	GetBySessionID(ctx context.Context, sessionID string) (orderdomain.Cart, error)
}

type TenantProductRepository interface {
	ProductRepository
	CreateWithTenant(ctx context.Context, product catalog.Product, tenantID string) error
	ListByTenant(ctx context.Context, tenantID string, page, perPage int) (ListResult, error)
	GetByIDAndTenant(ctx context.Context, id uuid.UUID, tenantID string) (catalog.Product, error)
}

type TenantOrderRepository interface {
	OrderRepository
	CreateWithTenant(ctx context.Context, order orderdomain.Order, tenantID string) error
}
