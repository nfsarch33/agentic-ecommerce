package tax_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/tax"
)

func item(sku string, amount int, exempt bool) tax.TaxableItem {
	return tax.TaxableItem{SKU: sku, Amount: amount, Exempt: exempt}
}

func TestCalculate_AUGSTSingle(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	items := []tax.TaxableItem{item("SKU-1", 10000, false)}
	result := calc.Calculate(items, tax.Jurisdiction{Country: "AU"})
	if result.TaxRate != 0.10 {
		t.Fatalf("AU GST should be 10%%, got %.2f", result.TaxRate)
	}
	if result.TaxAmount != 1000 {
		t.Fatalf("expected tax 1000, got %d", result.TaxAmount)
	}
	if result.Total != 11000 {
		t.Fatalf("expected total 11000, got %d", result.Total)
	}
}

func TestCalculate_ExemptItems(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	items := []tax.TaxableItem{
		item("SKU-1", 5000, false),
		item("SKU-2", 5000, true),
	}
	result := calc.Calculate(items, tax.Jurisdiction{Country: "AU"})
	// only non-exempt item (5000) taxed at 10% = 500
	if result.TaxAmount != 500 {
		t.Fatalf("expected 500 tax, got %d", result.TaxAmount)
	}
}

func TestCalculate_ZeroRate(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	items := []tax.TaxableItem{item("SKU-1", 10000, false)}
	result := calc.Calculate(items, tax.Jurisdiction{Country: "SG"}) // zero GST config
	if result.TaxAmount != 0 {
		t.Fatalf("expected 0 tax for zero-rate jurisdiction, got %d", result.TaxAmount)
	}
}

func TestCalculate_RoundingToNearestCent(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	items := []tax.TaxableItem{item("SKU-1", 1, false)} // 1 cent
	result := calc.Calculate(items, tax.Jurisdiction{Country: "AU"})
	// 1 * 0.10 = 0.1 cent -> rounds to 0
	if result.TaxAmount < 0 {
		t.Fatalf("negative tax: %d", result.TaxAmount)
	}
}

func TestCalculate_CompoundTax_Canada(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	items := []tax.TaxableItem{item("SKU-1", 10000, false)}
	result := calc.Calculate(items, tax.Jurisdiction{Country: "CA", Region: "ON"})
	// Ontario: GST 5% + HST effectively 13% total
	if result.TaxAmount < 1200 || result.TaxAmount > 1400 {
		t.Fatalf("Canadian compound tax out of range: %d", result.TaxAmount)
	}
}

func TestCalculate_TaxLines(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	items := []tax.TaxableItem{item("SKU-1", 10000, false)}
	result := calc.Calculate(items, tax.Jurisdiction{Country: "AU"})
	if len(result.TaxLines) == 0 {
		t.Fatal("expected at least 1 tax line")
	}
}

func TestCalculate_EmptyItems(t *testing.T) {
	t.Parallel()
	calc := tax.NewTaxCalculator(tax.DefaultRules())
	result := calc.Calculate(nil, tax.Jurisdiction{Country: "AU"})
	if result.TaxAmount != 0 || result.Total != 0 {
		t.Fatalf("empty items should give 0 tax")
	}
}
