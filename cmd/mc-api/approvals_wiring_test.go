package main

import (
	"bytes"
	"log/slog"
	"os"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
)

// TestServerWiresApprovalStore pins review blocker 2 (mc-api half):
// newServer must assign the PGStore built from ECOMMERCE_DB_URL to
// server.approvals — nil there means the deprecated signal fallback records
// nothing and the publish gate later fails closed on those workflows.
// Mutant: dropping the approvals assignment in newServer fails this test.
func TestServerWiresApprovalStore(t *testing.T) {
	t.Setenv("ECOMMERCE_RATE_LIMIT_CAPACITY", "100")
	t.Setenv("ECOMMERCE_RATE_LIMIT_REFILL", "1s")
	t.Setenv("ECOMMERCE_REDIS_ADDR", "")
	// pgxpool.New is lazy: a syntactically valid DSN wires the store
	// without connecting.
	t.Setenv("ECOMMERCE_DB_URL", "postgres://wiring:probe@127.0.0.1:1/ecommerce?sslmode=disable")

	srv := newServer(
		slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
		inmemory.NewProductRepository(),
		inmemory.NewOrderRepository(),
		inmemory.NewCartRepository(),
	)
	if srv.approvals == nil {
		t.Fatal("server.approvals is nil: the signal-fallback approval recording is dead (blocker 2)")
	}

	t.Setenv("ECOMMERCE_DB_URL", "")
	srv = newServer(
		slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)),
		inmemory.NewProductRepository(),
		inmemory.NewOrderRepository(),
		inmemory.NewCartRepository(),
	)
	if srv.approvals != nil {
		t.Fatal("approvals should stay nil without ECOMMERCE_DB_URL (gate fully disabled mode)")
	}
	_ = os.Environ()
}
