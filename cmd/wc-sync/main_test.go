package main

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/woocommerce"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

func TestRunDryRun(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), noopChannel{})
	if err != nil {
		t.Fatalf("run: %v", err)
	}

	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.product_synced")) {
		t.Fatalf("log output = %s", buf.String())
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
	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.product_synced")) {
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

	logger := discardLogger()
	if err := run(context.Background(), logger, failingChannel{}); err == nil {
		t.Fatal("expected error when channel.UpsertProduct fails")
	}
}

// TestMainImplReturnsOneOnRunError exercises the failure branch of
// mainImpl. We point the real woocommerce client at a closed loopback
// port so the channel returns a connection-refused error and
// run() propagates it.
func TestMainImplReturnsOneOnRunError(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		switch key {
		case "ECOMMERCE_WC_BASE_URL":
			return "http://127.0.0.1:1" // closed port; connection refused
		case "ECOMMERCE_WC_CONSUMER_KEY":
			return "ck_test"
		case "ECOMMERCE_WC_CONSUMER_SECRET":
			return "cs_test"
		case "ECOMMERCE_SYNC_DRY_RUN":
			return ""
		default:
			return ""
		}
	}
	var buf bytes.Buffer
	if got := mainImpl(&buf, getenv); got != 1 {
		t.Fatalf("mainImpl exit=%d log=%s", got, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("wc-sync.failed")) {
		t.Fatalf("expected wc-sync.failed log, got %s", buf.String())
	}
}
