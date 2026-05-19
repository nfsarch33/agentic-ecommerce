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

// T-5040-1: exhaustive order lifecycle state machine transition table.

func TestOrderStateMachine_AllValidTransitions(t *testing.T) {
	t.Parallel()

	validTransitions := []struct {
		from Status
		to   Status
	}{
		{StatusPending, StatusPaid},
		{StatusPending, StatusCancelled},
		{StatusPending, StatusFailed},
		{StatusPaid, StatusFulfilled},
		{StatusPaid, StatusCancelled},
		{StatusPaid, StatusFailed},
		{StatusFulfilled, StatusShipped},
		{StatusFulfilled, StatusFailed},
		{StatusShipped, StatusCompleted},
		// idempotent: same status transitions
		{StatusPending, StatusPending},
		{StatusPaid, StatusPaid},
		{StatusFulfilled, StatusFulfilled},
		{StatusShipped, StatusShipped},
		{StatusCompleted, StatusCompleted},
		{StatusFailed, StatusFailed},
		{StatusCancelled, StatusCancelled},
	}

	for _, tc := range validTransitions {
		tc := tc
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			t.Parallel()
			ord := advanceOrderTo(t, tc.from)
			if err := ord.AdvanceStatus(tc.to); err != nil {
				t.Fatalf("AdvanceStatus(%q -> %q): unexpected error: %v", tc.from, tc.to, err)
			}
		})
	}
}

func TestOrderStateMachine_AllInvalidTransitions(t *testing.T) {
	t.Parallel()

	invalidTransitions := []struct {
		from Status
		to   Status
	}{
		// pending cannot jump forward beyond paid/cancel/fail
		{StatusPending, StatusFulfilled},
		{StatusPending, StatusShipped},
		{StatusPending, StatusCompleted},
		// paid cannot go back or skip
		{StatusPaid, StatusPending},
		{StatusPaid, StatusShipped},
		{StatusPaid, StatusCompleted},
		// fulfilled cannot go back or cancel
		{StatusFulfilled, StatusPending},
		{StatusFulfilled, StatusPaid},
		{StatusFulfilled, StatusCancelled},
		{StatusFulfilled, StatusCompleted},
		// shipped cannot go back or cancel
		{StatusShipped, StatusPending},
		{StatusShipped, StatusPaid},
		{StatusShipped, StatusFulfilled},
		{StatusShipped, StatusCancelled},
		{StatusShipped, StatusFailed},
		// terminal states cannot transition to anything other than themselves
		{StatusCompleted, StatusPending},
		{StatusCompleted, StatusPaid},
		{StatusCompleted, StatusFulfilled},
		{StatusCompleted, StatusShipped},
		{StatusCompleted, StatusFailed},
		{StatusCompleted, StatusCancelled},
		{StatusFailed, StatusPending},
		{StatusFailed, StatusPaid},
		{StatusCancelled, StatusPending},
		{StatusCancelled, StatusPaid},
	}

	for _, tc := range invalidTransitions {
		tc := tc
		t.Run(string(tc.from)+"->"+string(tc.to), func(t *testing.T) {
			t.Parallel()
			ord := advanceOrderTo(t, tc.from)
			err := ord.AdvanceStatus(tc.to)
			if !errors.Is(err, ErrInvalidStatusTransition) {
				t.Fatalf("AdvanceStatus(%q -> %q): want ErrInvalidStatusTransition, got %v", tc.from, tc.to, err)
			}
		})
	}
}

// advanceOrderTo returns an order whose status equals target by following
// a valid transition path. Panics via t.Fatalf if the target is unreachable.
func advanceOrderTo(t *testing.T, target Status) Order {
	t.Helper()
	ord := testOrder(t)
	path := statusPath(target)
	for _, s := range path {
		if err := ord.AdvanceStatus(s); err != nil {
			t.Fatalf("advance to %q: %v", s, err)
		}
	}
	return ord
}

// statusPath returns the shortest valid transition sequence to reach target
// starting from StatusPending.
func statusPath(target Status) []Status {
	switch target {
	case StatusPending:
		return nil
	case StatusPaid:
		return []Status{StatusPaid}
	case StatusFulfilled:
		return []Status{StatusPaid, StatusFulfilled}
	case StatusShipped:
		return []Status{StatusPaid, StatusFulfilled, StatusShipped}
	case StatusCompleted:
		return []Status{StatusPaid, StatusFulfilled, StatusShipped, StatusCompleted}
	case StatusFailed:
		return []Status{StatusFailed}
	case StatusCancelled:
		return []Status{StatusCancelled}
	default:
		return nil
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
