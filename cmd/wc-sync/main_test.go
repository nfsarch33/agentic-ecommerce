package main

import (
	"bytes"
	"context"
	"log/slog"
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
