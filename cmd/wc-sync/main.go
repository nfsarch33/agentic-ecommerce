package main

import (
	"context"
	"log/slog"
	"os"

	appcatalog "github.com/nfsarch33/agentic-ecommerce/internal/app/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

type noopChannel struct{}

func (noopChannel) UpsertProduct(context.Context, catalog.Product) error {
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if err := run(context.Background(), logger, noopChannel{}); err != nil {
		logger.Error("wc-sync.failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, channel noopChannel) error {
	service := appcatalog.NewSyncService(channel)

	product, err := service.SyncProduct(ctx, catalog.ProductInput{
		SKU:         "DEMO-001",
		Title:       "Demo Product",
		Description: "Demo WooCommerce sync payload; configure adapters before live use.",
	})
	if err != nil {
		return err
	}

	logger.Info("wc-sync.dry_run", "sku", product.SKU())
	return nil
}
