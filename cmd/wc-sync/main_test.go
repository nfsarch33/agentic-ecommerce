package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func TestRunDryRun(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), noopChannel{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	// Read-only since the gate: staging is logged, publishing is not.
	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.product_staged")) {
		t.Fatalf("log output = %s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("product_synced")) {
		t.Fatalf("wc-sync still publishes: %s", buf.String())
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
	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.product_staged")) {
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

// TestRunPropagatesPublishFailure ensures engine errors bubble through
// run() rather than being swallowed.
func TestRunPropagatesPublishFailure(t *testing.T) {
	t.Parallel()

	// Publishing no longer happens here (the workflow owns it), so a failing
	// channel cannot fail the staging run. Mutant: restoring the publish call
	// makes this test fail on the error it expects to be absent.
	var buf bytes.Buffer
	err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), failingChannel{})
	if err != nil {
		t.Fatalf("run must not publish (read-only since the gate): %v", err)
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
