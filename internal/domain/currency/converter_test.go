package currency_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/currency"
)

func TestConvert_USDtoEUR(t *testing.T) {
	t.Parallel()
	store := currency.NewInMemoryRateStore(map[string]float64{
		"USD/EUR": 0.92,
	})
	conv := currency.NewCurrencyConverter(store)
	result, err := conv.Convert(currency.Money{Amount: 10000, Currency: "USD"}, "EUR")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if result.Currency != "EUR" {
		t.Fatalf("wrong currency: %s", result.Currency)
	}
	if result.Amount != 9200 {
		t.Fatalf("expected 9200, got %d", result.Amount)
	}
}

func TestConvert_SameCurrency_Identity(t *testing.T) {
	t.Parallel()
	store := currency.NewInMemoryRateStore(nil)
	conv := currency.NewCurrencyConverter(store)
	result, err := conv.Convert(currency.Money{Amount: 5000, Currency: "AUD"}, "AUD")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if result.Amount != 5000 {
		t.Fatalf("expected 5000, got %d", result.Amount)
	}
}

func TestConvert_UnknownCurrency_Error(t *testing.T) {
	t.Parallel()
	store := currency.NewInMemoryRateStore(nil)
	conv := currency.NewCurrencyConverter(store)
	_, err := conv.Convert(currency.Money{Amount: 100, Currency: "USD"}, "XYZ")
	if err == nil {
		t.Fatal("expected error for unknown currency")
	}
}

func TestConvert_ZeroAmount(t *testing.T) {
	t.Parallel()
	store := currency.NewInMemoryRateStore(map[string]float64{"USD/EUR": 0.92})
	conv := currency.NewCurrencyConverter(store)
	result, err := conv.Convert(currency.Money{Amount: 0, Currency: "USD"}, "EUR")
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if result.Amount != 0 {
		t.Fatalf("expected 0, got %d", result.Amount)
	}
}

func TestFormatDisplay_USD(t *testing.T) {
	t.Parallel()
	m := currency.Money{Amount: 123456, Currency: "USD"}
	display := currency.FormatDisplay(m)
	if display != "$1,234.56" {
		t.Fatalf("got %q", display)
	}
}

func TestFormatDisplay_EUR(t *testing.T) {
	t.Parallel()
	m := currency.Money{Amount: 123456, Currency: "EUR"}
	display := currency.FormatDisplay(m)
	if display != "€1,234.56" {
		t.Fatalf("got %q", display)
	}
}

func TestFormatDisplay_JPY(t *testing.T) {
	t.Parallel()
	m := currency.Money{Amount: 1234, Currency: "JPY"}
	display := currency.FormatDisplay(m)
	if display != "¥1,234" {
		t.Fatalf("got %q", display)
	}
}
