package catalog

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestNewProduct_RequiresIdentityAndTitle(t *testing.T) {
	t.Parallel()

	_, err := NewProduct(ProductInput{SKU: " ", Title: "Resistance Band"})
	if err == nil {
		t.Fatal("expected missing sku error")
	}

	_, err = NewProduct(ProductInput{SKU: "BAND-001", Title: " "})
	if err == nil {
		t.Fatal("expected missing title error")
	}
}

func TestNewProduct_NormalisesSKUAndTitle(t *testing.T) {
	t.Parallel()

	product, err := NewProduct(ProductInput{
		SKU:   " band-001 ",
		Title: "  Resistance Band  ",
	})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}

	if product.SKU() != "BAND-001" {
		t.Fatalf("SKU() = %q, want BAND-001", product.SKU())
	}
	if product.Title() != "Resistance Band" {
		t.Fatalf("Title() = %q, want Resistance Band", product.Title())
	}
}

func TestNewProduct_RejectsUnsafePublishCopy(t *testing.T) {
	t.Parallel()

	_, err := NewProduct(ProductInput{
		SKU:         "BAND-001",
		Title:       "Resistance Band",
		Description: strings.Repeat("x", MaxDescriptionRunes+1),
	})
	if err == nil {
		t.Fatal("expected long description error")
	}
}

func TestNewProduct_GeneratesUUID(t *testing.T) {
	t.Parallel()

	p, err := NewProduct(ProductInput{SKU: "BAND-001", Title: "Resistance Band"})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	if p.ID() == uuid.Nil {
		t.Fatal("expected non-nil UUID")
	}
}

func TestNewProduct_DefaultsToDraft(t *testing.T) {
	t.Parallel()

	p, err := NewProduct(ProductInput{SKU: "BAND-001", Title: "Resistance Band"})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	if p.Status() != StatusDraft {
		t.Fatalf("Status() = %q, want draft", p.Status())
	}
}

func TestNewProduct_GeneratesSlugFromTitle(t *testing.T) {
	t.Parallel()

	p, err := NewProduct(ProductInput{SKU: "BAND-001", Title: "Resistance Band Set"})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	if p.Slug() != "resistance-band-set" {
		t.Fatalf("Slug() = %q, want resistance-band-set", p.Slug())
	}
}

func TestNewProduct_UsesExplicitSlug(t *testing.T) {
	t.Parallel()

	p, err := NewProduct(ProductInput{
		SKU:   "BAND-001",
		Title: "Resistance Band",
		Slug:  "custom-slug",
	})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	if p.Slug() != "custom-slug" {
		t.Fatalf("Slug() = %q, want custom-slug", p.Slug())
	}
}

func TestNewProduct_SetsTimestamps(t *testing.T) {
	t.Parallel()

	before := time.Now().UTC().Add(-time.Second)
	p, err := NewProduct(ProductInput{SKU: "BAND-001", Title: "Resistance Band"})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	after := time.Now().UTC().Add(time.Second)

	if p.CreatedAt().Before(before) || p.CreatedAt().After(after) {
		t.Fatalf("CreatedAt() = %v, expected between %v and %v", p.CreatedAt(), before, after)
	}
	if p.UpdatedAt().Before(before) || p.UpdatedAt().After(after) {
		t.Fatalf("UpdatedAt() = %v, expected between %v and %v", p.UpdatedAt(), before, after)
	}
}

func TestNewProduct_AcceptsPrice(t *testing.T) {
	t.Parallel()

	price, _ := NewMoney(4995, "AUD")
	p, err := NewProduct(ProductInput{
		SKU:   "BAND-001",
		Title: "Resistance Band",
		Price: price,
		Stock: 10,
	})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	if p.Price().Amount() != 4995 || p.Price().Currency() != "AUD" {
		t.Fatalf("Price() = %v, want 4995 AUD", p.Price())
	}
	if p.Stock() != 10 {
		t.Fatalf("Stock() = %d, want 10", p.Stock())
	}
}

func TestReconstructProduct_PreservesAllFields(t *testing.T) {
	t.Parallel()

	id := uuid.New()
	price, _ := NewMoney(2500, "USD")
	now := time.Now().UTC()

	p := ReconstructProduct(ProductRecord{
		ID:          id,
		SKU:         "FOAM-001",
		Title:       "Foam Roller",
		Slug:        "foam-roller",
		Description: "Dense recovery roller",
		Price:       price,
		Stock:       5,
		Status:      StatusActive,
		Images:      []Image{{URL: "https://cdn.example/foam.jpg", Alt: "Foam roller"}},
		Categories:  []Category{{ID: uuid.New(), Name: "Recovery", Slug: "recovery"}},
		CreatedAt:   now,
		UpdatedAt:   now,
	})

	if p.ID() != id {
		t.Fatalf("ID() = %v, want %v", p.ID(), id)
	}
	if p.SKU() != "FOAM-001" {
		t.Fatalf("SKU() = %q, want FOAM-001", p.SKU())
	}
	if p.Status() != StatusActive {
		t.Fatalf("Status() = %q, want active", p.Status())
	}
	if len(p.Images()) != 1 || p.Images()[0].URL != "https://cdn.example/foam.jpg" {
		t.Fatalf("Images() = %v", p.Images())
	}
	if len(p.Categories()) != 1 || p.Categories()[0].Name != "Recovery" {
		t.Fatalf("Categories() = %v", p.Categories())
	}
}

func TestGenerateSlug(t *testing.T) {
	t.Parallel()

	tests := []struct {
		title string
		want  string
	}{
		{"Resistance Band Set", "resistance-band-set"},
		{"  Foam Roller  ", "foam-roller"},
		{"Pro's Gym Mat!", "pros-gym-mat"},
	}
	for _, tc := range tests {
		got := generateSlug(tc.title)
		if got != tc.want {
			t.Errorf("generateSlug(%q) = %q, want %q", tc.title, got, tc.want)
		}
	}
}
