package catalog

import (
	"strings"
	"testing"
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
