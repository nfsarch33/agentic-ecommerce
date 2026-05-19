package port

import (
	"context"

	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

type ProductChannel interface {
	UpsertProduct(ctx context.Context, product catalog.Product) error
}
