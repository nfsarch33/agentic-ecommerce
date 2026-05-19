package postgres

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

func TestProductRepositoryIntegrationCRUD(t *testing.T) {
	dsn := os.Getenv("ECOMMERCE_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("set ECOMMERCE_POSTGRES_TEST_DSN to run PostgreSQL integration tests")
	}

	ctx := context.Background()
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	defer pool.Close()

	migration, err := os.ReadFile(filepath.Join("..", "..", "..", "migrations", "0001_create_products.up.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	if _, err := pool.Exec(ctx, string(migration)); err != nil {
		t.Fatalf("apply migration: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(context.Background(), "TRUNCATE product_categories, product_images, products, categories RESTART IDENTITY CASCADE")
	})
	if _, err := pool.Exec(ctx, "TRUNCATE product_categories, product_images, products, categories RESTART IDENTITY CASCADE"); err != nil {
		t.Fatalf("truncate catalog tables: %v", err)
	}

	repo := NewProductRepository(pool)
	product := integrationProduct(t, "INT-001", "Integration Product", 1200)
	if err := repo.Create(ctx, product); err != nil {
		t.Fatalf("create product: %v", err)
	}

	found, err := repo.GetBySlug(ctx, product.Slug())
	if err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if found.ID() != product.ID() {
		t.Fatalf("found ID = %s, want %s", found.ID(), product.ID())
	}

	updated := catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          product.ID(),
		SKU:         product.SKU(),
		Title:       "Integration Product Updated",
		Slug:        product.Slug(),
		Description: product.Description(),
		Price:       product.Price(),
		Stock:       42,
		Status:      catalog.StatusActive,
		CreatedAt:   product.CreatedAt(),
		UpdatedAt:   time.Now().UTC(),
	})
	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("update product: %v", err)
	}

	list, err := repo.List(ctx, 1, 10)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if list.Total != 1 || len(list.Products) != 1 {
		t.Fatalf("list = total %d len %d, want one product", list.Total, len(list.Products))
	}

	if err := repo.Delete(ctx, product.ID()); err != nil {
		t.Fatalf("delete product: %v", err)
	}
	if _, err := repo.GetByID(ctx, product.ID()); err != ErrProductNotFound {
		t.Fatalf("get deleted err = %v, want ErrProductNotFound", err)
	}
}

func integrationProduct(t *testing.T, sku, title string, amount int) catalog.Product {
	t.Helper()
	price, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	now := time.Now().UTC()
	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          uuid.New(),
		SKU:         sku,
		Title:       title,
		Slug:        "integration-product",
		Description: "PostgreSQL integration test product",
		Price:       price,
		Stock:       10,
		Status:      catalog.StatusDraft,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}
