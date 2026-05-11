//go:build integration_pg

// File scope: v6.1.0 Story 2 -- cmd/* binaries depend on a live
// Postgres pool through internal/adapter/postgres. The smoke test
// here pins the contract end-to-end: an ephemeral Postgres container
// boots, the migration list applies, and a sample composition-root
// dependency (the WebhookIdempotencyStore wired in v6.1.0 CF-15)
// behaves as the production composition does.
package cmdintegration

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgImage    = "postgres:16-alpine"
	pgDatabase = "ecommerce_cmd_test"
	pgUser     = "ecommerce"
	pgPassword = "ecommerce"
)

// migrationFiles mirrors the canonical migrate-up sequence used by
// the postgres adapter integration suite. Keep these in sync.
var migrationFiles = []string{
	"0001_create_products.up.sql",
	"0002_create_orders.up.sql",
	"0003_create_product_media_assets.up.sql",
	"0004_add_tenant_id.up.sql",
	"0005_enable_pgvector_rag.up.sql",
	"0006_tenant_settings_compliance_reporting.up.sql",
	"0007_membership.up.sql",
	"0008_digital.up.sql",
	"0009_marketplace.up.sql",
	"0010_billing.up.sql",
	"0011_rls.up.sql",
	"0037_idempotency_store.up.sql",
}

func startContainerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("DISABLE_DOCKER_TESTCONTAINERS") == "1" {
		t.Skip("DISABLE_DOCKER_TESTCONTAINERS=1; skipping testcontainers integration test")
	}

	migrationDir, err := filepath.Abs(filepath.Join("..", "..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	migrationPaths := make([]string, 0, len(migrationFiles))
	for _, name := range migrationFiles {
		migrationPaths = append(migrationPaths, filepath.Join(migrationDir, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		pgImage,
		tcpostgres.WithDatabase(pgDatabase),
		tcpostgres.WithUsername(pgUser),
		tcpostgres.WithPassword(pgPassword),
		tcpostgres.WithInitScripts(migrationPaths...),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("testcontainers postgres unavailable (likely no Docker): %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	strat := wait.ForLog("database system is ready to accept connections").WithStartupTimeout(60 * time.Second)
	if err := strat.WaitUntilReady(ctx, container); err != nil {
		t.Skipf("postgres readiness wait failed: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("connection string: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)

	if err := pool.Ping(ctx); err != nil {
		t.Skipf("postgres ping failed: %v", err)
	}
	return pool
}

// TestIntegrationCmdMcApiPostgresPoolWiring pins the v6.1.0 Story 2
// contract: the mc-api composition root can stand up an end-to-end
// Postgres pool against a real container and the v6.1.0
// WebhookIdempotencyStore drives the same SQL the production binary
// would execute.
func TestIntegrationCmdMcApiPostgresPoolWiring(t *testing.T) {
	pool := startContainerPool(t)
	store := postgres.NewWebhookIdempotencyStore(pool)

	ok, err := store.Reserve(context.Background(), "tenant-cmd-int", "key-1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if !ok {
		t.Fatal("Reserve first observation: got false, want true")
	}
	ok, err = store.Reserve(context.Background(), "tenant-cmd-int", "key-1")
	if err != nil {
		t.Fatalf("Reserve dup: %v", err)
	}
	if ok {
		t.Fatal("Reserve duplicate: got true, want false")
	}
}
