package catalog

import (
	"errors"
	"testing"
)

func TestNewMoney_ValidCurrencies(t *testing.T) {
	t.Parallel()

	for _, cur := range []string{"AUD", "USD", "GBP", "EUR"} {
		m, err := NewMoney(100, cur)
		if err != nil {
			t.Fatalf("NewMoney(100, %q): %v", cur, err)
		}
		if m.Amount() != 100 || m.Currency() != cur {
			t.Fatalf("got (%d, %q), want (100, %q)", m.Amount(), m.Currency(), cur)
		}
	}
}

func TestNewMoney_NormalisesCurrency(t *testing.T) {
	t.Parallel()

	m, err := NewMoney(50, " aud ")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	if m.Currency() != "AUD" {
		t.Fatalf("Currency() = %q, want AUD", m.Currency())
	}
}

func TestNewMoney_RejectsNegative(t *testing.T) {
	t.Parallel()

	_, err := NewMoney(-1, "AUD")
	if !errors.Is(err, ErrNegativeAmount) {
		t.Fatalf("expected ErrNegativeAmount, got %v", err)
	}
}

func TestNewMoney_RejectsInvalidCurrency(t *testing.T) {
	t.Parallel()

	_, err := NewMoney(100, "XYZ")
	if !errors.Is(err, ErrInvalidCurrency) {
		t.Fatalf("expected ErrInvalidCurrency, got %v", err)
	}
}

func TestMoney_IsZero(t *testing.T) {
	t.Parallel()

	zero := ZeroAUD()
	if !zero.IsZero() {
		t.Fatal("expected IsZero() = true")
	}

	nonZero, _ := NewMoney(1, "AUD")
	if nonZero.IsZero() {
		t.Fatal("expected IsZero() = false")
	}
}
