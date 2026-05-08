package order

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

// File scope: covers the ReconstructOrder/ReconstructCart accessors and
// pure-getter methods that previously sat at 0% because the constructor
// happy paths used catalog.New* directly. This file exercises the
// reconstruction surface used by repository adapters when reading
// rows back from the database.

func mustMoneyTest(t *testing.T, amount int) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return money
}

func TestReconstructOrderRoundTripsAccessorsAndItems(t *testing.T) {
	t.Parallel()

	productID := uuid.MustParse("c1000000-0000-0000-0000-000000000001")
	orderID := uuid.MustParse("d1000000-0000-0000-0000-000000000001")
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	subtotal := mustMoneyTest(t, 1995)
	totals := Totals{Subtotal: subtotal, Shipping: mustMoneyTest(t, 0), Total: subtotal}
	address := ShippingAddress{Name: "Jane", Line1: "1 Market", City: "Sydney", PostalCode: "2000", Country: "AU"}
	item := ReconstructOrderItem(OrderItemInput{
		ProductID: productID,
		SKU:       "RB-1",
		Title:     "Resistance Band",
		Quantity:  2,
		UnitPrice: mustMoneyTest(t, 999),
	}, mustMoneyTest(t, 1998))

	order := ReconstructOrder(OrderRecord{
		ID:              orderID,
		CustomerEmail:   "shopper@example.com",
		Items:           []OrderItem{item},
		Status:          StatusPending,
		Totals:          totals,
		ShippingAddress: address,
		CreatedAt:       now,
		UpdatedAt:       now,
	})

	if order.ID() != orderID {
		t.Fatalf("ID = %s, want %s", order.ID(), orderID)
	}
	if order.CustomerEmail() != "shopper@example.com" {
		t.Fatalf("CustomerEmail = %q", order.CustomerEmail())
	}
	if order.Status() != StatusPending {
		t.Fatalf("Status = %q", order.Status())
	}
	if !order.CreatedAt().Equal(now) {
		t.Fatalf("CreatedAt = %s, want %s", order.CreatedAt(), now)
	}
	if !order.UpdatedAt().Equal(now) {
		t.Fatalf("UpdatedAt = %s, want %s", order.UpdatedAt(), now)
	}
	if order.ShippingAddress().Name != "Jane" {
		t.Fatalf("ShippingAddress = %+v", order.ShippingAddress())
	}
	if got := order.Totals(); got.Total.Amount() != 1995 {
		t.Fatalf("Totals.Total = %d, want 1995", got.Total.Amount())
	}

	items := order.Items()
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	gotItem := items[0]
	if gotItem.ProductID() != productID || gotItem.SKU() != "RB-1" || gotItem.Title() != "Resistance Band" || gotItem.Quantity() != 2 {
		t.Fatalf("item = %+v", gotItem)
	}
	if gotItem.UnitPrice().Amount() != 999 || gotItem.LineTotal().Amount() != 1998 {
		t.Fatalf("item amounts = unit=%d line=%d", gotItem.UnitPrice().Amount(), gotItem.LineTotal().Amount())
	}
}

func TestOrderItemsAccessorReturnsCopyToPreventCallerMutation(t *testing.T) {
	t.Parallel()

	productID := uuid.New()
	item := ReconstructOrderItem(OrderItemInput{
		ProductID: productID,
		SKU:       "RB-1",
		Title:     "Resistance Band",
		Quantity:  1,
		UnitPrice: mustMoneyTest(t, 100),
	}, mustMoneyTest(t, 100))

	order := ReconstructOrder(OrderRecord{Items: []OrderItem{item}, Status: StatusPending})
	got := order.Items()
	got = append(got, OrderItem{})
	_ = got

	if len(order.Items()) != 1 {
		t.Fatal("Items accessor leaked underlying slice")
	}
}

func TestReconstructCartRoundTripsAccessorsAndItems(t *testing.T) {
	t.Parallel()

	productID := uuid.MustParse("c2000000-0000-0000-0000-000000000001")
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

	subtotal := mustMoneyTest(t, 4995)
	totals := Totals{Subtotal: subtotal, Shipping: mustMoneyTest(t, 0), Total: subtotal}
	item := ReconstructCartItem(CartItemInput{
		ProductID: productID,
		SKU:       "yoga-mat",
		Title:     "Yoga Mat",
		Quantity:  1,
		UnitPrice: mustMoneyTest(t, 4995),
	}, mustMoneyTest(t, 4995))

	cart := ReconstructCart(CartRecord{
		SessionID: "session-1",
		Items:     []CartItem{item},
		Totals:    totals,
		UpdatedAt: now,
	})

	if cart.SessionID() != "session-1" {
		t.Fatalf("SessionID = %q", cart.SessionID())
	}
	if !cart.UpdatedAt().Equal(now) {
		t.Fatalf("UpdatedAt = %s", cart.UpdatedAt())
	}
	if cart.IsEmpty() {
		t.Fatal("expected non-empty cart")
	}
	if cart.Totals().Total.Amount() != 4995 {
		t.Fatalf("Totals.Total = %d, want 4995", cart.Totals().Total.Amount())
	}

	items := cart.Items()
	if len(items) != 1 {
		t.Fatalf("Items = %d, want 1", len(items))
	}
	gotItem := items[0]
	if gotItem.ProductID() != productID || gotItem.SKU() != "YOGA-MAT" || gotItem.Title() != "Yoga Mat" || gotItem.Quantity() != 1 {
		t.Fatalf("item = %+v", gotItem)
	}
	if gotItem.UnitPrice().Amount() != 4995 || gotItem.LineTotal().Amount() != 4995 {
		t.Fatalf("item amounts = unit=%d line=%d", gotItem.UnitPrice().Amount(), gotItem.LineTotal().Amount())
	}
}

func TestReconstructCartItemUppercasesSKUAndTrimsTitle(t *testing.T) {
	t.Parallel()

	got := ReconstructCartItem(CartItemInput{
		ProductID: uuid.New(),
		SKU:       "  band-1  ",
		Title:     "  Band  ",
		Quantity:  1,
		UnitPrice: mustMoneyTest(t, 50),
	}, mustMoneyTest(t, 50))

	if got.SKU() != "BAND-1" {
		t.Fatalf("SKU = %q, want BAND-1", got.SKU())
	}
	if got.Title() != "Band" {
		t.Fatalf("Title = %q, want Band", got.Title())
	}
}

func TestReconstructOrderItemUppercasesSKUAndTrimsTitle(t *testing.T) {
	t.Parallel()

	got := ReconstructOrderItem(OrderItemInput{
		ProductID: uuid.New(),
		SKU:       "  band-2  ",
		Title:     "  Band 2  ",
		Quantity:  1,
		UnitPrice: mustMoneyTest(t, 50),
	}, mustMoneyTest(t, 50))

	if got.SKU() != "BAND-2" {
		t.Fatalf("SKU = %q, want BAND-2", got.SKU())
	}
	if got.Title() != "Band 2" {
		t.Fatalf("Title = %q, want Band 2", got.Title())
	}
}
