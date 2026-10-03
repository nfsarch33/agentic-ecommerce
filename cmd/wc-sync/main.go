package main

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	pgproduct "github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
)

type noopChannel struct{}

func (noopChannel) UpsertProduct(_ context.Context, _ catalog.Product) error {
	return nil
}

func (noopChannel) ListProducts(_ context.Context, _ woocommerce.ListOptions) ([]woocommerce.Product, error) {
	return nil, nil
}

func main() {
	os.Exit(mainImpl(os.Stdout, os.Getenv))
}

// mainImpl is the testable entry point. It returns the process exit
// code so main() reduces to os.Exit(mainImpl(...)). Following the
// go-clean-architecture pattern, all dependency-construction is here
// and tests inject getenv + a writer instead of swapping globals.
func mainImpl(stdout io.Writer, getenv func(string) string) int {
	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	channel := channelFromEnv(logger, getenv)
	if err := run(context.Background(), logger, channel); err != nil {
		logger.Error("wc-sync.failed", "error", err)
		return 1
	}
	return 0
}

// run pulls the store's products into Postgres through the sync engine,
// page by page, until a short page says the catalogue is exhausted. The
// engine makes the loop idempotent: a SKU that already exists locally is
// conflict-checked and skipped, never re-created, so a second run over an
// unchanged store imports nothing and conflicts nothing.
func run(ctx context.Context, logger *slog.Logger, channel enginesync.WooCommerceClient) error {
	return runWith(ctx, logger, channel, openRepoFromEnv)
}

// countingChannel remembers the size of the last page the engine fetched,
// which is the driver's only signal that the catalogue is exhausted (the
// engine returns imported/conflict counts, not page sizes).
type countingChannel struct {
	enginesync.WooCommerceClient
	lastLen int
}

func (c *countingChannel) ListProducts(ctx context.Context, opts woocommerce.ListOptions) ([]woocommerce.Product, error) {
	out, err := c.WooCommerceClient.ListProducts(ctx, opts)
	c.lastLen = len(out)
	return out, err
}

// openRepoFromEnv opens the Postgres product repository; without a DSN it
// returns a nil repo and the run degrades to the dry-run log (a sync tool
// must not silently sync into a throwaway store).
func openRepoFromEnv(ctx context.Context) (port.ProductRepository, func(), error) {
	dsn := strings.TrimSpace(os.Getenv("ECOMMERCE_DB_URL"))
	if dsn == "" {
		return nil, func() {}, nil
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, nil, fmt.Errorf("create sync pool: %w", err)
	}
	return pgproduct.NewProductRepository(pool), pool.Close, nil
}

func runWith(ctx context.Context, logger *slog.Logger, channel enginesync.WooCommerceClient, openRepo func(context.Context) (port.ProductRepository, func(), error)) error {
	repo, closeRepo, err := openRepo(ctx)
	if err != nil {
		return err
	}
	defer closeRepo()
	if repo == nil {
		logger.Info("wc-sync.dry_run", "reason", "ECOMMERCE_DB_URL not set; nothing synced")
		return nil
	}

	eng := enginesync.NewEngine(enginesync.Config{
		ProductRepository: repo,
		WooCommerce:       channel,
		DefaultCurrency:   "AUD",
		Now:               time.Now,
	})
	cc := &countingChannel{WooCommerceClient: channel}
	eng = enginesync.NewEngine(enginesync.Config{
		ProductRepository: repo,
		WooCommerce:       cc,
		DefaultCurrency:   "AUD",
		Now:               time.Now,
	})

	const perPage = 100
	var pages, imported, conflicts int
	for page := 1; ; page++ {
		res, err := eng.ImportFromWooCommerce(ctx, enginesync.ImportOptions{Page: page, PerPage: perPage})
		if err != nil {
			return fmt.Errorf("page %d: %w", page, err)
		}
		pages++
		imported += res.Imported
		conflicts += res.Conflicts
		if cc.lastLen < perPage {
			break
		}
	}
	logger.Info("wc-sync.synced",
		"pages", pages,
		"imported", imported,
		"conflicts", conflicts,
		"note", "existing SKUs are conflict-checked and skipped; an unchanged store imports nothing on a second run")
	return nil
}

func channelFromEnv(logger *slog.Logger, getenv func(string) string) enginesync.WooCommerceClient {
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
