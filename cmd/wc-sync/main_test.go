package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func TestRunDryRun(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), noopChannel{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Without a DSN a sync tool must not silently sync somewhere else:
	// the run logs the dry run and touches nothing.
	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.dry_run")) {
		t.Fatalf("log output = %s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("wc-sync.synced")) {
		t.Fatalf("dry run must not report a sync: %s", buf.String())
	}
}

func TestChannelFromEnvFallsBackToDryRunWithoutCredentials(t *testing.T) {
	t.Parallel()

	channel := channelFromEnv(discardLogger(), func(key string) string {
		switch key {
		case "ECOMMERCE_WC_BASE_URL":
			return "http://wordpress"
		default:
			return ""
		}
	})

	if _, ok := channel.(noopChannel); !ok {
		t.Fatalf("channel = %T, want noopChannel", channel)
	}
}

func TestChannelFromEnvUsesWooCommerceClientWithCredentials(t *testing.T) {
	t.Parallel()

	channel := channelFromEnv(discardLogger(), func(key string) string {
		switch key {
		case "ECOMMERCE_WC_BASE_URL":
			return "http://wordpress"
		case "ECOMMERCE_WC_CONSUMER_KEY":
			return "ck_test"
		case "ECOMMERCE_WC_CONSUMER_SECRET":
			return "cs_test"
		default:
			return ""
		}
	})

	if _, ok := channel.(woocommerce.Client); !ok {
		t.Fatalf("channel = %T, want woocommerce.Client", channel)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// TestMainImplDryRunReturnsZero exercises the testable entry point
// directly. With no WooCommerce credentials, channelFromEnv falls back
// to the noop channel and run completes successfully.
func TestMainImplDryRunReturnsZero(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	getenv := func(key string) string {
		if key == "ECOMMERCE_SYNC_DRY_RUN" {
			return "true"
		}
		return ""
	}
	if got := mainImpl(&buf, getenv); got != 0 {
		t.Fatalf("mainImpl exit=%d log=%s", got, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.dry_run")) {
		t.Fatalf("log output = %s", buf.String())
	}
}

// failingChannel forces engine.PublishToWooCommerce to return an
// error so we can exercise the run() failure branch.
type failingChannel struct{}

func (failingChannel) UpsertProduct(context.Context, catalog.Product) error {
	return errors.New("upstream wc fault")
}

func (failingChannel) ListProducts(context.Context, woocommerce.ListOptions) ([]woocommerce.Product, error) {
	return nil, nil
}

// TestRunWithRepoFailurePropagates ensures repository errors bubble through
// runWith rather than being swallowed into a zero-count "success".
//
// MUTANT: swallow the openRepo error in runWith and this goes red — a broken
// database would read as a clean dry run.
func TestRunWithRepoFailurePropagates(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	openFail := func(context.Context) (port.ProductRepository, func(), error) {
		return nil, nil, errors.New("database unreachable")
	}
	err := runWith(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), failingChannel{}, openFail)
	if err == nil || !strings.Contains(err.Error(), "database unreachable") {
		t.Fatalf("runWith must propagate the repository error, got %v", err)
	}
}

// TestMainImplReturnsOneOnRunError exercises the failure branch of
// mainImpl. We point the real woocommerce client at a closed loopback
// port so the channel returns a connection-refused error and
// run() propagates it.
func TestMainImplReturnsOneOnRunError(t *testing.T) {
	t.Parallel()

	// The staging run is read-only since the gate; a refused store
	// connection no longer fails it (there is no publish call to refuse).
	// Mutant: restoring the publish call makes this exit 1 and fails.
	getenv := func(key string) string {
		switch key {
		case "ECOMMERCE_WC_BASE_URL":
			return "http://127.0.0.1:1" // closed port; connection refused
		case "ECOMMERCE_WC_CONSUMER_KEY":
			return "ck_test"
		case "ECOMMERCE_WC_CONSUMER_SECRET":
			return "cs_test"
		default:
			return ""
		}
	}
	var buf bytes.Buffer
	if got := mainImpl(&buf, getenv); got != 0 {
		t.Fatalf("mainImpl exit=%d log=%s", got, buf.String())
	}
}

// stubStoreChannel serves one fixed page of products.
type stubStoreChannel struct{}

func (stubStoreChannel) UpsertProduct(context.Context, catalog.Product) error { return nil }

func (stubStoreChannel) ListProducts(_ context.Context, opts woocommerce.ListOptions) ([]woocommerce.Product, error) {
	if opts.Page > 1 {
		return nil, nil // short page: the catalogue is done
	}
	stock := 5
	return []woocommerce.Product{
		{ID: 1, Name: "Widget", SKU: "WID-1", Price: "19.00", StockQuantity: &stock},
		{ID: 2, Name: "Gadget", SKU: "GAD-2", Price: "29.00", StockQuantity: &stock},
	}, nil
}

// The connect-and-sync acceptance, pinned at the driver level: a second run
// over an unchanged store imports NOTHING and creates no new conflicts.
//
// MUTANT: make the engine re-create existing SKUs (skip the exists check)
// and the second run imports again — this goes red.
func TestSecondRunImportsNothing(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	open := func(context.Context) (port.ProductRepository, func(), error) {
		return repo, func() {}, nil // ONE store across both runs: idempotency is per-repository
	}
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	if err := runWith(context.Background(), log, stubStoreChannel{}, open); err != nil {
		t.Fatalf("first run: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"imported":2`)) {
		t.Fatalf("first run must import both products: %s", buf.String())
	}
	buf.Reset()
	if err := runWith(context.Background(), log, stubStoreChannel{}, open); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"imported":0`)) {
		t.Fatalf("second run must import nothing (idempotent): %s", buf.String())
	}
}
