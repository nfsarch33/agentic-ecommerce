package order

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

func TestNewCart_RequiresSessionID(t *testing.T) {
	t.Parallel()

	_, err := NewCart(" ")
	if !errors.Is(err, ErrMissingSessionID) {
		t.Fatalf("NewCart err = %v, want ErrMissingSessionID", err)
	}
}

func TestCartReplaceItems_ValidatesAndTotalsItems(t *testing.T) {
	t.Parallel()
	cart, err := NewCart("session-123")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}

	if err := cart.ReplaceItems([]CartItemInput{
		testCartItem(t, "BAND-001", 2, 2495),
		testCartItem(t, "ROLLER-001", 1, 3500),
	}); err != nil {
		t.Fatalf("ReplaceItems: %v", err)
	}

	if cart.SessionID() != "session-123" {
		t.Fatalf("SessionID() = %q", cart.SessionID())
	}
	if len(cart.Items()) != 2 {
		t.Fatalf("len Items() = %d, want 2", len(cart.Items()))
	}
	if cart.Totals().Subtotal.Amount() != 8490 || cart.Totals().Total.Amount() != 8490 {
		t.Fatalf("Totals() = %+v, want 8490", cart.Totals())
	}
	if cart.IsEmpty() {
		t.Fatal("cart should not be empty")
	}
}

func TestCartReplaceItems_RejectsInvalidQuantityAndCurrencyMix(t *testing.T) {
	t.Parallel()
	cart, err := NewCart("session-123")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}

	if err := cart.ReplaceItems([]CartItemInput{testCartItem(t, "BAND-001", 0, 2495)}); !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("invalid quantity err = %v, want ErrInvalidQuantity", err)
	}

	aud := testCartItem(t, "BAND-001", 1, 2495)
	usdPrice, _ := catalog.NewMoney(2500, "USD")
	usd := CartItemInput{ProductID: uuid.New(), SKU: "USD-001", Title: "USD Item", Quantity: 1, UnitPrice: usdPrice}
	if err := cart.ReplaceItems([]CartItemInput{aud, usd}); !errors.Is(err, ErrCurrencyMismatch) {
		t.Fatalf("currency mismatch err = %v, want ErrCurrencyMismatch", err)
	}
}

func TestCartReplaceItems_EmptyClearsCart(t *testing.T) {
	t.Parallel()
	cart, err := NewCart("session-123")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	_ = cart.ReplaceItems([]CartItemInput{testCartItem(t, "BAND-001", 1, 2495)})

	if err := cart.ReplaceItems(nil); err != nil {
		t.Fatalf("ReplaceItems nil: %v", err)
	}
	if !cart.IsEmpty() {
		t.Fatal("cart should be empty")
	}
	if cart.Totals().Total.Amount() != 0 {
		t.Fatalf("total = %d, want 0", cart.Totals().Total.Amount())
	}
}

func testCartItem(t *testing.T, sku string, quantity int, amount int) CartItemInput {
	t.Helper()
	price, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	return CartItemInput{
		ProductID: uuid.New(),
		SKU:       sku,
		Title:     "Cart Item",
		Quantity:  quantity,
		UnitPrice: price,
	}
}
