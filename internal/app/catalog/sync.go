package catalog

import (
	"context"
	"fmt"

	domain "github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

type SyncService struct {
	channel port.ProductChannel
}

func NewSyncService(channel port.ProductChannel) SyncService {
	return SyncService{channel: channel}
}

func (s SyncService) SyncProduct(ctx context.Context, input domain.ProductInput) (domain.Product, error) {
	product, err := domain.NewProduct(input)
	if err != nil {
		return domain.Product{}, err
	}

	if err := s.channel.UpsertProduct(ctx, product); err != nil {
		return domain.Product{}, fmt.Errorf("upsert product %s: %w", product.SKU(), err)
	}

	return product, nil
}
