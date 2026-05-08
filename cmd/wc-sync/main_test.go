package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"os"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
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

// TestMainSucceedsInDryRun exercises the entrypoint that wires
// os.Stdout, os.Getenv, and run together. Without WooCommerce
// credentials in the test environment, channelFromEnv falls back to the
// dry-run noop channel and run completes successfully -- so main()
// returns without calling os.Exit. We swap os.Stdout for a pipe to keep
// test output clean.
func TestMainSucceedsInDryRun(t *testing.T) {
	t.Setenv("ECOMMERCE_WC_BASE_URL", "")
	t.Setenv("ECOMMERCE_WC_CONSUMER_KEY", "")
	t.Setenv("ECOMMERCE_WC_CONSUMER_SECRET", "")
	t.Setenv("ECOMMERCE_SYNC_DRY_RUN", "true")

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("os.Pipe: %v", err)
	}
	originalStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() {
		os.Stdout = originalStdout
		_ = r.Close()
	})

	main()

	if err := w.Close(); err != nil {
		t.Fatalf("close pipe writer: %v", err)
	}
	captured, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	if !bytes.Contains(captured, []byte("wc-sync.product_synced")) {
		t.Fatalf("main stdout = %s", string(captured))
	}
}
