package shipping_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/shipping"
)

func dest(country string) shipping.Address {
	return shipping.Address{Country: country, PostalCode: "2000"}
}

func TestCalculate_DomesticStandard(t *testing.T) {
	t.Parallel()
	calc := shipping.NewShippingCalculator(shipping.DefaultRates())
	cart := shipping.Cart{WeightGrams: 500}
	options, err := calc.Calculate(cart, dest("AU"))
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if len(options) == 0 {
		t.Fatal("expected at least 1 domestic option")
	}
}

func TestCalculate_InternationalZone(t *testing.T) {
	t.Parallel()
	calc := shipping.NewShippingCalculator(shipping.DefaultRates())
	cart := shipping.Cart{WeightGrams: 1000}
	options, err := calc.Calculate(cart, dest("US"))
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	if len(options) == 0 {
		t.Fatal("expected international options")
	}
	for _, o := range options {
		if o.Cost.Amount <= 0 {
			t.Fatalf("international shipping should have a cost")
		}
	}
}

func TestCalculate_FreeShippingThreshold(t *testing.T) {
	t.Parallel()
	rates := shipping.DefaultRates()
	rates.FreeThresholdCents = 10000
	calc := shipping.NewShippingCalculator(rates)
	cart := shipping.Cart{WeightGrams: 500, OrderValueCents: 12000}
	options, err := calc.Calculate(cart, dest("AU"))
	if err != nil {
		t.Fatalf("Calculate: %v", err)
	}
	hasFree := false
	for _, o := range options {
		if o.Cost.Amount == 0 {
			hasFree = true
		}
	}
	if !hasFree {
		t.Fatal("expected free shipping option above threshold")
	}
}

func TestCalculate_OverweightSurcharge(t *testing.T) {
	t.Parallel()
	calc := shipping.NewShippingCalculator(shipping.DefaultRates())
	heavy := shipping.Cart{WeightGrams: 25000}
	light := shipping.Cart{WeightGrams: 500}
	heavyOpts, _ := calc.Calculate(heavy, dest("AU"))
	lightOpts, _ := calc.Calculate(light, dest("AU"))
	if heavyOpts[0].Cost.Amount <= lightOpts[0].Cost.Amount {
		t.Fatalf("heavy cart should cost more: heavy=%d, light=%d", heavyOpts[0].Cost.Amount, lightOpts[0].Cost.Amount)
	}
}

func TestCalculate_EmptyCart(t *testing.T) {
	t.Parallel()
	calc := shipping.NewShippingCalculator(shipping.DefaultRates())
	cart := shipping.Cart{WeightGrams: 0}
	_, err := calc.Calculate(cart, dest("AU"))
	if err == nil {
		t.Fatal("expected error for empty/zero-weight cart")
	}
}

func TestCalculate_UnknownCountry_Defaults(t *testing.T) {
	t.Parallel()
	calc := shipping.NewShippingCalculator(shipping.DefaultRates())
	cart := shipping.Cart{WeightGrams: 500}
	options, err := calc.Calculate(cart, dest("ZZ"))
	// unknown country falls into zone 4 (rest of world)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(options) == 0 {
		t.Fatal("expected fallback options for unknown country")
	}
}
