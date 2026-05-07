package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	appcatalog "github.com/nfsarch33/agentic-ecommerce/internal/app/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type noopChannel struct{}

func (noopChannel) UpsertProduct(context.Context, catalog.Product) error {
	return nil
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	channel := channelFromEnv(logger, os.Getenv)
	if err := run(context.Background(), logger, channel); err != nil {
		logger.Error("wc-sync.failed", "error", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, logger *slog.Logger, channel port.ProductChannel) error {
	service := appcatalog.NewSyncService(channel)

	product, err := service.SyncProduct(ctx, catalog.ProductInput{
		SKU:         "DEMO-001",
		Title:       "Demo Product",
		Description: "Demo WooCommerce sync payload; configure adapters before live use.",
		Price:       catalog.ZeroAUD(),
		Status:      catalog.StatusDraft,
	})
	if err != nil {
		return err
	}

	logger.Info("wc-sync.product_synced", "sku", product.SKU())
	return nil
}

func channelFromEnv(logger *slog.Logger, getenv func(string) string) port.ProductChannel {
	baseURL := strings.TrimSpace(getenv("ECOMMERCE_WC_BASE_URL"))
	consumerKey := strings.TrimSpace(getenv("ECOMMERCE_WC_CONSUMER_KEY"))
	consumerSecret := strings.TrimSpace(getenv("ECOMMERCE_WC_CONSUMER_SECRET"))
	dryRun := strings.ToLower(strings.TrimSpace(getenv("ECOMMERCE_SYNC_DRY_RUN")))

	missingCredentials := baseURL == "" || consumerKey == "" || consumerSecret == ""
	if dryRun == "true" || dryRun == "1" || missingCredentials {
		logger.Info("wc-sync.dry_run_enabled", "dry_run", true, "missing_credentials", missingCredentials)
		return noopChannel{}
	}

	return woocommerce.NewClient(woocommerce.Config{
		BaseURL:        baseURL,
		ConsumerKey:    consumerKey,
		ConsumerSecret: consumerSecret,
	}, &http.Client{Timeout: 10 * time.Second})
}
