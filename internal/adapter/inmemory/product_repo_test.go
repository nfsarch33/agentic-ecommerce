package inmemory

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func seedProduct(t *testing.T, sku, title string) catalog.Product {
	t.Helper()
	price, _ := catalog.NewMoney(2500, "AUD")
	p, err := catalog.NewProduct(catalog.ProductInput{
		SKU:   sku,
		Title: title,
		Price: price,
		Stock: 10,
	})
	if err != nil {
		t.Fatalf("seed product: %v", err)
	}
	return p
}

func TestCreate_StoresProduct(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	p := seedProduct(t, "BAND-001", "Resistance Band")
	if err := repo.Create(ctx, p); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(ctx, p.ID())
	if err != nil {
		t.Fatalf("get by id: %v", err)
	}
	if got.SKU() != "BAND-001" {
		t.Fatalf("SKU() = %q, want BAND-001", got.SKU())
	}
}

func TestCreate_RejectsDuplicate(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	p := seedProduct(t, "BAND-001", "Resistance Band")
	_ = repo.Create(ctx, p)

	err := repo.Create(ctx, p)
	if !errors.Is(err, ErrDuplicateProduct) {
		t.Fatalf("expected ErrDuplicateProduct, got %v", err)
	}
}

func TestGetBySlug_FindsProduct(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	p := seedProduct(t, "FOAM-001", "Foam Roller")
	_ = repo.Create(ctx, p)

	got, err := repo.GetBySlug(ctx, p.Slug())
	if err != nil {
		t.Fatalf("get by slug: %v", err)
	}
	if got.ID() != p.ID() {
		t.Fatalf("ID() = %v, want %v", got.ID(), p.ID())
	}
}

func TestGetBySlug_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()

	_, err := repo.GetBySlug(context.Background(), "nonexistent")
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestGetByID_ReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()

	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestList_PaginatesResults(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	for i := 0; i < 5; i++ {
		p := seedProduct(t, "SKU-"+string(rune('A'+i)), "Product "+string(rune('A'+i)))
		_ = repo.Create(ctx, p)
		time.Sleep(time.Millisecond)
	}

	result, err := repo.List(ctx, 1, 2)
	if err != nil {
		t.Fatalf("list page 1: %v", err)
	}
	if result.Total != 5 {
		t.Fatalf("Total = %d, want 5", result.Total)
	}
	if len(result.Products) != 2 {
		t.Fatalf("page 1 len = %d, want 2", len(result.Products))
	}

	result, err = repo.List(ctx, 3, 2)
	if err != nil {
		t.Fatalf("list page 3: %v", err)
	}
	if len(result.Products) != 1 {
		t.Fatalf("page 3 len = %d, want 1", len(result.Products))
	}
}

func TestList_EmptyOnBeyondPage(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	p := seedProduct(t, "BAND-001", "Band")
	_ = repo.Create(ctx, p)

	result, err := repo.List(ctx, 10, 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(result.Products) != 0 {
		t.Fatalf("expected empty page, got %d", len(result.Products))
	}
	if result.Total != 1 {
		t.Fatalf("Total = %d, want 1", result.Total)
	}
}

func TestTenantProductMethodsIsolateProducts(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	productA := seedProduct(t, "TENANT-A-SKU", "Tenant A Product")
	productB := seedProduct(t, "TENANT-B-SKU", "Tenant B Product")
	if err := repo.CreateWithTenant(ctx, productA, "tenant-a"); err != nil {
		t.Fatalf("create tenant A product: %v", err)
	}
	if err := repo.CreateWithTenant(ctx, productB, "tenant-b"); err != nil {
		t.Fatalf("create tenant B product: %v", err)
	}

	listA, err := repo.ListByTenant(ctx, "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("list tenant A products: %v", err)
	}
	if listA.Total != 1 || listA.Products[0].ID() != productA.ID() {
		t.Fatalf("tenant A list = %+v, want only %s", listA, productA.ID())
	}

	if _, err := repo.GetByIDAndTenant(ctx, productA.ID(), "tenant-b"); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("cross-tenant get error = %v, want ErrProductNotFound", err)
	}
}

func TestUpdate_ModifiesProduct(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	p := seedProduct(t, "BAND-001", "Resistance Band")
	_ = repo.Create(ctx, p)

	price, _ := catalog.NewMoney(5000, "AUD")
	updated := catalog.ReconstructProduct(catalog.ProductRecord{
		ID:        p.ID(),
		SKU:       p.SKU(),
		Title:     "Updated Band",
		Slug:      "updated-band",
		Price:     price,
		Stock:     20,
		Status:    catalog.StatusActive,
		CreatedAt: p.CreatedAt(),
		UpdatedAt: time.Now().UTC(),
	})

	if err := repo.Update(ctx, updated); err != nil {
		t.Fatalf("update: %v", err)
	}

	got, _ := repo.GetByID(ctx, p.ID())
	if got.Title() != "Updated Band" {
		t.Fatalf("Title() = %q after update", got.Title())
	}
	if got.Price().Amount() != 5000 {
		t.Fatalf("Price().Amount() = %d after update", got.Price().Amount())
	}
}

func TestUpdate_NotFound(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()

	p := seedProduct(t, "BAND-001", "Band")
	err := repo.Update(context.Background(), p)
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

func TestDelete_RemovesProduct(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()
	ctx := context.Background()

	p := seedProduct(t, "BAND-001", "Band")
	_ = repo.Create(ctx, p)

	if err := repo.Delete(ctx, p.ID()); err != nil {
		t.Fatalf("delete: %v", err)
	}

	_, err := repo.GetByID(ctx, p.ID())
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound after delete, got %v", err)
	}
}

func TestDelete_NotFound(t *testing.T) {
	t.Parallel()
	repo := NewProductRepository()

	err := repo.Delete(context.Background(), uuid.New())
	if !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}
