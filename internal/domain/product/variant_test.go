package product_test

import (
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/product"
)

func TestVariant_GenerateCombinations(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	combos := vm.GenerateCombinations(map[string][]string{
		"color": {"red", "blue"},
		"size":  {"S", "M"},
	})
	if len(combos) != 4 {
		t.Fatalf("expected 4 combinations, got %d", len(combos))
	}
}

func TestVariant_SKUGeneration(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	sku := vm.GenerateSKU("BASE", map[string]string{"color": "red", "size": "M"})
	if !strings.HasPrefix(sku, "BASE-") {
		t.Fatalf("expected SKU to start with BASE-, got %s", sku)
	}
}

func TestVariant_AddAndGet(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	v := product.Variant{
		SKU:        "SHIRT-RED-M",
		ProductID:  "P1",
		Attributes: map[string]string{"color": "red", "size": "M"},
		Price:      2999,
		Stock:      10,
	}
	vm.Add(v)
	got, err := vm.Get("SHIRT-RED-M")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Price != 2999 {
		t.Fatalf("expected price 2999, got %d", got.Price)
	}
}

func TestVariant_NotFound(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	_, err := vm.Get("NOEXIST")
	if err == nil {
		t.Fatal("expected error for missing variant")
	}
}

func TestVariant_UpdateStock(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	vm.Add(product.Variant{SKU: "V1", ProductID: "P1", Attributes: map[string]string{}, Price: 100, Stock: 5})
	if err := vm.UpdateStock("V1", 3); err != nil {
		t.Fatalf("UpdateStock: %v", err)
	}
	got, _ := vm.Get("V1")
	if got.Stock != 3 {
		t.Fatalf("expected stock 3, got %d", got.Stock)
	}
}

func TestVariant_ListByProduct(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	vm.Add(product.Variant{SKU: "A1", ProductID: "P1", Attributes: map[string]string{}, Price: 100, Stock: 1})
	vm.Add(product.Variant{SKU: "A2", ProductID: "P1", Attributes: map[string]string{}, Price: 200, Stock: 2})
	vm.Add(product.Variant{SKU: "B1", ProductID: "P2", Attributes: map[string]string{}, Price: 300, Stock: 3})
	result := vm.ListByProduct("P1")
	if len(result) != 2 {
		t.Fatalf("expected 2 variants for P1, got %d", len(result))
	}
}

func TestVariant_DuplicateSKU(t *testing.T) {
	t.Parallel()
	vm := product.NewVariantManager()
	v := product.Variant{SKU: "DUP", ProductID: "P1", Attributes: map[string]string{}, Price: 100, Stock: 1}
	vm.Add(v)
	err := vm.Add(v)
	if err == nil {
		t.Fatal("expected error on duplicate SKU")
	}
}
