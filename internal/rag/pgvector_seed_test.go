//go:build integration_pg

package rag_test

// File scope: a seeded-fixture test for the pgvector adapter that runs
// against an ephemeral PostgreSQL container with the pgvector extension
// installed. Gated behind the `integration_pg` build tag so the default
// `go test ./...` path stays Docker-free, matching the postgres adapter
// integration test pattern.
//
// Run with:
//
//   runx make integration-pg --repo ecommerce
//
// Self-skips when Docker is unreachable.

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgVectorImage = "pgvector/pgvector:pg16"
	pgDatabase    = "ecommerce_rag_test"
	pgUser        = "ecommerce"
	pgPassword    = "ecommerce"
)

var ragMigrationFiles = []string{
	"0001_create_products.up.sql",
	"0002_create_orders.up.sql",
	"0003_create_product_media_assets.up.sql",
	"0004_add_tenant_id.up.sql",
	"0005_enable_pgvector_rag.up.sql",
	"0006_tenant_settings_compliance_reporting.up.sql",
}

func startContainerPool(t *testing.T) *pgxpool.Pool {
	t.Helper()

	if os.Getenv("DISABLE_DOCKER_TESTCONTAINERS") == "1" {
		t.Skip("DISABLE_DOCKER_TESTCONTAINERS=1; skipping pgvector seeded fixture test")
	}

	migrationDir, err := filepath.Abs(filepath.Join("..", "..", "migrations"))
	if err != nil {
		t.Fatalf("resolve migrations dir: %v", err)
	}
	migrationPaths := make([]string, 0, len(ragMigrationFiles))
	for _, name := range ragMigrationFiles {
		migrationPaths = append(migrationPaths, filepath.Join(migrationDir, name))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	container, err := tcpostgres.Run(
		ctx,
		pgVectorImage,
		tcpostgres.WithDatabase(pgDatabase),
		tcpostgres.WithUsername(pgUser),
		tcpostgres.WithPassword(pgPassword),
		tcpostgres.WithInitScripts(migrationPaths...),
		tcpostgres.BasicWaitStrategies(),
	)
	if err != nil {
		t.Skipf("pgvector container unavailable (likely no Docker): %v", err)
	}
	t.Cleanup(func() {
		_ = container.Terminate(context.Background())
	})

	if err := container.Start(ctx); err != nil && err.Error() != "container is already started" {
		t.Skipf("pgvector container failed to start: %v", err)
	}

	strat := wait.ForLog("database system is ready to accept connections").WithStartupTimeout(60 * time.Second)
	if err := strat.WaitUntilReady(ctx, container); err != nil {
		t.Skipf("pgvector readiness wait failed: %v", err)
	}

	dsn, err := container.ConnectionString(ctx, "sslmode=disable")
	if err != nil {
		t.Fatalf("dsn: %v", err)
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("pgxpool.New: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("pgvector ping failed: %v", err)
	}
	return pool
}

func TestPGVectorStoreSeededFixtureUpsertsAndSearches(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	store := rag.NewPGVectorStore(pool, 4)
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)

	chunks := []rag.EmbeddedChunk{
		{
			Chunk: rag.Chunk{
				ID:         "chunk-resistance",
				DocumentID: "doc-resistance",
				TenantID:   "tenant-a",
				Index:      0,
				Title:      "Resistance Band Set",
				Source:     "supplier-spec",
				Text:       "Resistance bands ship as a set of five colour-coded levels",
				Metadata:   map[string]string{"sku": "RB"},
				CreatedAt:  now,
			},
			Embedding: []float64{1, 0, 0, 0},
		},
		{
			Chunk: rag.Chunk{
				ID:         "chunk-yoga",
				DocumentID: "doc-yoga",
				TenantID:   "tenant-a",
				Index:      0,
				Title:      "Yoga Mat",
				Source:     "supplier-spec",
				Text:       "High-density TPE yoga mat available in two thickness profiles",
				Metadata:   map[string]string{"sku": "YM"},
				CreatedAt:  now,
			},
			Embedding: []float64{0, 1, 0, 0},
		},
	}

	if err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	results, err := store.Search(ctx, rag.SearchQuery{
		TenantID:  "tenant-a",
		Embedding: []float64{0.95, 0.05, 0, 0},
		TopK:      2,
	})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) == 0 || results[0].ChunkID != "chunk-resistance" {
		t.Fatalf("top result = %+v, want chunk-resistance first", results)
	}

	// Verify tenant isolation: searching as tenant-b returns no chunks.
	emptyResults, err := store.Search(ctx, rag.SearchQuery{
		TenantID:  "tenant-b",
		Embedding: []float64{0.95, 0.05, 0, 0},
		TopK:      5,
	})
	if err != nil {
		t.Fatalf("Search tenant-b: %v", err)
	}
	if len(emptyResults) != 0 {
		t.Fatalf("tenant-b leak: got %d results, want 0", len(emptyResults))
	}

	// Asserts the unique constraint on (tenant_id, source_uri) by re-upserting with a
	// new embedding and ensuring the chunk count stays stable.
	chunks[0].Embedding = []float64{0.5, 0.5, 0, 0}
	if err := store.UpsertChunks(ctx, chunks); err != nil {
		t.Fatalf("re-UpsertChunks: %v", err)
	}
	var count int
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM rag_document_chunks").Scan(&count); err != nil {
		t.Fatalf("count chunks: %v", err)
	}
	if count != 2 {
		t.Fatalf("chunk count after re-upsert = %d, want 2 (UPSERT, not duplicated)", count)
	}
	_ = fmt.Sprintf // keep fmt import if test grows
}
