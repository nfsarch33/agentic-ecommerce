package inmemory

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
)

func TestOrderRepositoryCreateAndGetByID(t *testing.T) {
	t.Parallel()
	repo := NewOrderRepository()
	ctx := context.Background()
	ord := inmemoryTestOrder(t)

	if err := repo.Create(ctx, ord); err != nil {
		t.Fatalf("Create: %v", err)
	}

	got, err := repo.GetByID(ctx, ord.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID() != ord.ID() || got.CustomerEmail() != ord.CustomerEmail() {
		t.Fatalf("order = %s/%s, want %s/%s", got.ID(), got.CustomerEmail(), ord.ID(), ord.CustomerEmail())
	}
}

func TestOrderRepositoryUpdateStatus(t *testing.T) {
	t.Parallel()
	repo := NewOrderRepository()
	ctx := context.Background()
	ord := inmemoryTestOrder(t)
	_ = repo.Create(ctx, ord)

	got, err := repo.UpdateStatus(ctx, ord.ID(), orderdomain.StatusPaid)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if got.Status() != orderdomain.StatusPaid {
		t.Fatalf("Status() = %q, want paid", got.Status())
	}

	got, err = repo.GetByID(ctx, ord.ID())
	if err != nil {
		t.Fatalf("GetByID after update: %v", err)
	}
	if got.Status() != orderdomain.StatusPaid {
		t.Fatalf("persisted Status() = %q, want paid", got.Status())
	}
}

func TestOrderRepositoryUpdateStatusRejectsInvalidTransition(t *testing.T) {
	t.Parallel()
	repo := NewOrderRepository()
	ctx := context.Background()
	ord := inmemoryTestOrder(t)
	_ = repo.Create(ctx, ord)

	if _, err := repo.UpdateStatus(ctx, ord.ID(), orderdomain.StatusFulfilled); !errors.Is(err, orderdomain.ErrInvalidStatusTransition) {
		t.Fatalf("UpdateStatus err = %v, want ErrInvalidStatusTransition", err)
	}
}

func TestOrderRepositoryGetByIDReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := NewOrderRepository()

	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("GetByID err = %v, want ErrOrderNotFound", err)
	}
}

func TestCartRepositorySaveAndGet(t *testing.T) {
	t.Parallel()
	repo := NewCartRepository()
	ctx := context.Background()
	cart := inmemoryTestCart(t)

	if err := repo.Save(ctx, cart); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := repo.GetBySessionID(ctx, cart.SessionID())
	if err != nil {
		t.Fatalf("GetBySessionID: %v", err)
	}
	if got.SessionID() != cart.SessionID() || got.Totals().Total.Amount() != cart.Totals().Total.Amount() {
		t.Fatalf("cart = %s/%d, want %s/%d", got.SessionID(), got.Totals().Total.Amount(), cart.SessionID(), cart.Totals().Total.Amount())
	}
}

func TestCartRepositoryReturnsEmptyCartForNewSession(t *testing.T) {
	t.Parallel()
	repo := NewCartRepository()

	got, err := repo.GetBySessionID(context.Background(), "new-session")
	if err != nil {
		t.Fatalf("GetBySessionID: %v", err)
	}
	if got.SessionID() != "new-session" || !got.IsEmpty() {
		t.Fatalf("cart = %s empty=%v, want empty new-session cart", got.SessionID(), got.IsEmpty())
	}
}

func inmemoryTestOrder(t *testing.T) orderdomain.Order {
	t.Helper()
	ord, err := orderdomain.NewOrder(orderdomain.OrderInput{
		CustomerEmail: "shopper@example.com",
		Items: []orderdomain.OrderItemInput{{
			ProductID: uuid.New(),
			SKU:       "BAND-001",
			Title:     "Resistance Band",
			Quantity:  1,
			UnitPrice: mustMoney(t, 2495),
		}},
		ShippingAddress: orderdomain.ShippingAddress{Name: "Jane Shopper", Line1: "1 Market Street", City: "Sydney", Region: "NSW", PostalCode: "2000", Country: "AU"},
	})
	if err != nil {
		t.Fatalf("NewOrder: %v", err)
	}
	return ord
}

func inmemoryTestCart(t *testing.T) orderdomain.Cart {
	t.Helper()
	cart, err := orderdomain.NewCart("session-123")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := cart.ReplaceItems([]orderdomain.CartItemInput{{
		ProductID: uuid.New(),
		SKU:       "BAND-001",
		Title:     "Resistance Band",
		Quantity:  2,
		UnitPrice: mustMoney(t, 2495),
	}}); err != nil {
		t.Fatalf("ReplaceItems: %v", err)
	}
	return cart
}

func mustMoney(t *testing.T, amount int) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return money
}
