package port

import (
	"context"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

type ProductChannel interface {
	UpsertProduct(ctx context.Context, product catalog.Product) error
}
