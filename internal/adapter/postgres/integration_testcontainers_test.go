//go:build integration_pg

package postgres_test

// File scope: testcontainers-driven integration tests for the postgres
// adapter. Each test boots its own ephemeral PostgreSQL container so there
// is no shared database between tests, no test ordering coupling, and no
// reliance on external infrastructure. Gated behind the `integration_pg`
// build tag so the default `go test ./...` path keeps the unit-only
// runtime profile.
//
// Run with:
//
//   runx make integration-pg --repo ecommerce
//
// Requires Docker. Skips automatically if Docker is unreachable.

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"
)

const (
	pgImage    = "postgres:16-alpine"
	pgDatabase = "ecommerce_test"
	pgUser     = "ecommerce"
	pgPassword = "ecommerce"
)

// migrationFiles defines the ordered DDL applied to each ephemeral
// container. The list intentionally mirrors the migrate-up Make target so
// repository tests run against the same schema as production.
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

	if err := container.Start(ctx); err != nil && !isAlreadyStarted(err) {
		t.Skipf("postgres container failed to start: %v", err)
	}

	if err := waitForReady(ctx, container); err != nil {
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

func waitForReady(ctx context.Context, container *tcpostgres.PostgresContainer) error {
	strat := wait.ForLog("database system is ready to accept connections").WithStartupTimeout(60 * time.Second)
	return strat.WaitUntilReady(ctx, container)
}

func isAlreadyStarted(err error) bool {
	return err != nil && err.Error() == "container is already started"
}

func TestIntegrationProductRepositoryFullCRUDWithEphemeralPostgres(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	repo := postgres.NewProductRepository(pool)
	product := makeIntegrationProduct(t, "TC-PROD-1")

	if err := repo.Create(ctx, product); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, product.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.SKU() != product.SKU() {
		t.Fatalf("SKU = %s, want %s", got.SKU(), product.SKU())
	}

	updated := catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          product.ID(),
		SKU:         product.SKU(),
		Title:       "Updated Title",
		Slug:        product.Slug(),
		Description: product.Description(),
		Price:       product.Price(),
		Stock:       42,
		Status:      catalog.StatusActive,
		CreatedAt:   product.CreatedAt(),
		UpdatedAt:   time.Now().UTC(),
	})
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("Update: %v", err)
	}

	list, err := repo.List(ctx, 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if list.Total != 1 || len(list.Products) != 1 {
		t.Fatalf("List = total %d len %d, want 1", list.Total, len(list.Products))
	}

	if err := repo.Delete(ctx, product.ID()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := repo.GetByID(ctx, product.ID()); err != postgres.ErrProductNotFound {
		t.Fatalf("GetByID after delete = %v, want ErrProductNotFound", err)
	}
}

func TestIntegrationProductRepositoryTenantIsolationWithEphemeralPostgres(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	repo := postgres.NewProductRepository(pool)

	productA := makeIntegrationProduct(t, "TC-TENANT-A")
	productB := makeIntegrationProduct(t, "TC-TENANT-B")

	if err := repo.CreateWithTenant(ctx, productA, "tenant-a"); err != nil {
		t.Fatalf("CreateWithTenant tenant-a: %v", err)
	}
	if err := repo.CreateWithTenant(ctx, productB, "tenant-b"); err != nil {
		t.Fatalf("CreateWithTenant tenant-b: %v", err)
	}

	listA, err := repo.ListByTenant(ctx, "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("ListByTenant tenant-a: %v", err)
	}
	if listA.Total != 1 || listA.Products[0].SKU() != productA.SKU() {
		t.Fatalf("tenant-a list = %+v", listA)
	}

	if _, err := repo.GetByIDAndTenant(ctx, productB.ID(), "tenant-a"); err != postgres.ErrProductNotFound {
		t.Fatalf("cross-tenant read leaked product B: err=%v", err)
	}

	gotB, err := repo.GetByIDAndTenant(ctx, productB.ID(), "tenant-b")
	if err != nil {
		t.Fatalf("GetByIDAndTenant tenant-b: %v", err)
	}
	if gotB.SKU() != productB.SKU() {
		t.Fatalf("tenant-b read = %s, want %s", gotB.SKU(), productB.SKU())
	}
}

func TestIntegrationOrderRepositoryStatusTransitionWithEphemeralPostgres(t *testing.T) {
	pool := startContainerPool(t)
	ctx := context.Background()

	products := postgres.NewProductRepository(pool)
	orders := postgres.NewOrderRepository(pool)

	product := makeIntegrationProduct(t, "TC-ORDER-FK")
	if err := products.Create(ctx, product); err != nil {
		t.Fatalf("seed product: %v", err)
	}

	order := makeIntegrationOrder(t, product)
	if err := orders.Create(ctx, order); err != nil {
		t.Fatalf("orders.Create: %v", err)
	}

	got, err := orders.GetByID(ctx, order.ID())
	if err != nil {
		t.Fatalf("orders.GetByID: %v", err)
	}
	if got.CustomerEmail() != order.CustomerEmail() {
		t.Fatalf("customer = %q, want %q", got.CustomerEmail(), order.CustomerEmail())
	}

	updated, err := orders.UpdateStatus(ctx, order.ID(), orderdomain.StatusPaid)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if updated.Status() != orderdomain.StatusPaid {
		t.Fatalf("status = %q, want paid", updated.Status())
	}
}

func makeIntegrationProduct(t *testing.T, sku string) catalog.Product {
	t.Helper()
	price, err := catalog.NewMoney(2495, "AUD")
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	now := time.Now().UTC()
	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          uuid.New(),
		SKU:         sku,
		Title:       "Resistance Band Set " + sku,
		Slug:        "resistance-band-" + sku,
		Description: "Integration-test resistance band set used to verify postgres adapter behaviour.",
		Price:       price,
		Stock:       50,
		Status:      catalog.StatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

func makeIntegrationOrder(t *testing.T, product catalog.Product) orderdomain.Order {
	t.Helper()
	price, err := catalog.NewMoney(2495, "AUD")
	if err != nil {
		t.Fatalf("price: %v", err)
	}
	order, err := orderdomain.NewOrder(orderdomain.OrderInput{
		CustomerEmail: "shopper@example.com",
		Items: []orderdomain.OrderItemInput{{
			ProductID: product.ID(),
			SKU:       product.SKU(),
			Title:     product.Title(),
			Quantity:  1,
			UnitPrice: price,
		}},
		ShippingAddress: orderdomain.ShippingAddress{
			Name:       "Jane Shopper",
			Line1:      "1 Market Street",
			City:       "Sydney",
			Region:     "NSW",
			PostalCode: "2000",
			Country:    "AU",
		},
	})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return order
}
