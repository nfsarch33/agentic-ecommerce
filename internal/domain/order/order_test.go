package order

import (
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func TestNewOrder_ValidatesCustomerItemsAndAddress(t *testing.T) {
	t.Parallel()
	item := testOrderItem(t, "BAND-001", 2, 2495)
	address := testShippingAddress()

	ord, err := NewOrder(OrderInput{
		CustomerEmail:   " SHOPPER@Example.COM ",
		Items:           []OrderItemInput{item},
		ShippingAddress: address,
	})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}

	if ord.ID() == uuid.Nil {
		t.Fatal("expected non-nil order ID")
	}
	if ord.CustomerEmail() != "shopper@example.com" {
		t.Fatalf("CustomerEmail() = %q, want shopper@example.com", ord.CustomerEmail())
	}
	if ord.Status() != StatusPending {
		t.Fatalf("Status() = %q, want pending", ord.Status())
	}
	if ord.Totals().Subtotal.Amount() != 4990 || ord.Totals().Total.Amount() != 4990 {
		t.Fatalf("Totals() = %+v, want subtotal and total 4990", ord.Totals())
	}
	if ord.CreatedAt().IsZero() || ord.UpdatedAt().IsZero() {
		t.Fatal("expected timestamps to be set")
	}
}

func TestNewOrder_RejectsInvalidInput(t *testing.T) {
	t.Parallel()

	_, err := NewOrder(OrderInput{CustomerEmail: "not-an-email", Items: []OrderItemInput{testOrderItem(t, "BAND-001", 1, 1000)}, ShippingAddress: testShippingAddress()})
	if !errors.Is(err, ErrInvalidCustomerEmail) {
		t.Fatalf("invalid email err = %v, want ErrInvalidCustomerEmail", err)
	}

	_, err = NewOrder(OrderInput{CustomerEmail: "shopper@example.com", ShippingAddress: testShippingAddress()})
	if !errors.Is(err, ErrOrderRequiresItems) {
		t.Fatalf("missing items err = %v, want ErrOrderRequiresItems", err)
	}

	_, err = NewOrder(OrderInput{CustomerEmail: "shopper@example.com", Items: []OrderItemInput{testOrderItem(t, "BAND-001", 0, 1000)}, ShippingAddress: testShippingAddress()})
	if !errors.Is(err, ErrInvalidQuantity) {
		t.Fatalf("invalid quantity err = %v, want ErrInvalidQuantity", err)
	}
}

func TestOrderAdvanceStatus_FollowsCheckoutStateMachine(t *testing.T) {
	t.Parallel()
	ord := testOrder(t)

	for _, status := range []Status{StatusPaid, StatusFulfilled, StatusShipped, StatusCompleted} {
		if err := ord.AdvanceStatus(status); err != nil {
			t.Fatalf("AdvanceStatus(%q): %v", status, err)
		}
		if ord.Status() != status {
			t.Fatalf("Status() = %q, want %q", ord.Status(), status)
		}
	}

	if err := ord.AdvanceStatus(StatusCancelled); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("terminal transition err = %v, want ErrInvalidStatusTransition", err)
	}
}

func TestOrderAdvanceStatus_AllowsExplicitFailureAndCancellationRules(t *testing.T) {
	t.Parallel()

	pending := testOrder(t)
	if err := pending.AdvanceStatus(StatusCancelled); err != nil {
		t.Fatalf("pending cancellation should be allowed: %v", err)
	}

	paid := testOrder(t)
	if err := paid.AdvanceStatus(StatusPaid); err != nil {
		t.Fatalf("mark paid: %v", err)
	}
	if err := paid.AdvanceStatus(StatusCancelled); err != nil {
		t.Fatalf("paid cancellation should be allowed before fulfilment: %v", err)
	}

	fulfilled := testOrder(t)
	_ = fulfilled.AdvanceStatus(StatusPaid)
	_ = fulfilled.AdvanceStatus(StatusFulfilled)
	if err := fulfilled.AdvanceStatus(StatusCancelled); !errors.Is(err, ErrInvalidStatusTransition) {
		t.Fatalf("fulfilled cancellation err = %v, want ErrInvalidStatusTransition", err)
	}
	if err := fulfilled.AdvanceStatus(StatusFailed); err != nil {
		t.Fatalf("fulfilled failure should be allowed: %v", err)
	}
}

func TestParseStatus(t *testing.T) {
	t.Parallel()

	status, err := ParseStatus("paid")
	if err != nil {
		t.Fatalf("ParseStatus: %v", err)
	}
	if status != StatusPaid {
		t.Fatalf("status = %q, want paid", status)
	}

	if _, err := ParseStatus("refunded"); !errors.Is(err, ErrInvalidStatus) {
		t.Fatalf("invalid status err = %v, want ErrInvalidStatus", err)
	}
}

func testOrder(t *testing.T) Order {
	t.Helper()
	ord, err := NewOrder(OrderInput{
		CustomerEmail:   "shopper@example.com",
		Items:           []OrderItemInput{testOrderItem(t, "BAND-001", 1, 2495)},
		ShippingAddress: testShippingAddress(),
	})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return ord
}

func testOrderItem(t *testing.T, sku string, quantity int, amount int) OrderItemInput {
	t.Helper()
	price, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	return OrderItemInput{
		ProductID: uuid.New(),
		SKU:       sku,
		Title:     "Resistance Band",
		Quantity:  quantity,
		UnitPrice: price,
	}
}

func testShippingAddress() ShippingAddress {
	return ShippingAddress{
		Name:       "Jane Shopper",
		Line1:      "1 Market Street",
		City:       "Sydney",
		Region:     "NSW",
		PostalCode: "2000",
		Country:    "AU",
	}
}
