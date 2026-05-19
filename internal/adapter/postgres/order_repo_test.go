package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/helixon-ec/internal/domain/order"
)

func TestOrderRepositoryCreateExecsOrderAndItemInserts(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &OrderRepository{pool: pool}

	if err := repo.Create(context.Background(), postgresTestOrder(t)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(pool.execSQL) != 2 {
		t.Fatalf("exec calls = %d, want order insert and item insert", len(pool.execSQL))
	}
}

func TestOrderRepositoryGetByIDScansOrderWithItems(t *testing.T) {
	t.Parallel()
	ord := postgresTestOrder(t)
	pool := &fakePool{
		row:  fakeOrderRow(ord),
		rows: &fakeRows{rows: [][]any{fakeOrderItemValues(ord.Items()[0])}},
	}
	repo := &OrderRepository{pool: pool}

	got, err := repo.GetByID(context.Background(), ord.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	if got.ID() != ord.ID() || len(got.Items()) != 1 || got.Items()[0].SKU() != "BAND-001" {
		t.Fatalf("order = %s items=%d, want %s one BAND-001", got.ID(), len(got.Items()), ord.ID())
	}
}

func TestOrderRepositoryUpdateStatusPersistsValidTransition(t *testing.T) {
	t.Parallel()
	ord := postgresTestOrder(t)
	pool := &fakePool{
		row:        fakeOrderRow(ord),
		rows:       &fakeRows{rows: [][]any{fakeOrderItemValues(ord.Items()[0])}},
		commandTag: pgconn.NewCommandTag("UPDATE 1"),
	}
	repo := &OrderRepository{pool: pool}

	got, err := repo.UpdateStatus(context.Background(), ord.ID(), orderdomain.StatusPaid)
	if err != nil {
		t.Fatalf("UpdateStatus: %v", err)
	}
	if got.Status() != orderdomain.StatusPaid {
		t.Fatalf("Status() = %q, want paid", got.Status())
	}
}

func TestOrderRepositoryUpdateStatusRejectsInvalidTransitionBeforeExec(t *testing.T) {
	t.Parallel()
	ord := postgresTestOrder(t)
	pool := &fakePool{
		row:  fakeOrderRow(ord),
		rows: &fakeRows{rows: [][]any{fakeOrderItemValues(ord.Items()[0])}},
	}
	repo := &OrderRepository{pool: pool}

	_, err := repo.UpdateStatus(context.Background(), ord.ID(), orderdomain.StatusFulfilled)
	if !errors.Is(err, orderdomain.ErrInvalidStatusTransition) {
		t.Fatalf("UpdateStatus err = %v, want ErrInvalidStatusTransition", err)
	}
	if len(pool.execSQL) != 0 {
		t.Fatalf("exec calls = %d, want no update for invalid transition", len(pool.execSQL))
	}
}

func TestCartRepositorySaveUpsertsCartAndItems(t *testing.T) {
	t.Parallel()
	cart := postgresTestCart(t)
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &CartRepository{pool: pool}

	if err := repo.Save(context.Background(), cart); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if len(pool.execSQL) != 3 {
		t.Fatalf("exec calls = %d, want cart upsert, item delete, item insert", len(pool.execSQL))
	}
}

func TestCartRepositoryGetBySessionIDScansCartWithItems(t *testing.T) {
	t.Parallel()
	cart := postgresTestCart(t)
	pool := &fakePool{
		row:  fakeCartRow(cart),
		rows: &fakeRows{rows: [][]any{fakeCartItemValues(cart.Items()[0])}},
	}
	repo := &CartRepository{pool: pool}

	got, err := repo.GetBySessionID(context.Background(), cart.SessionID())
	if err != nil {
		t.Fatalf("GetBySessionID: %v", err)
	}
	if got.SessionID() != cart.SessionID() || len(got.Items()) != 1 || got.Totals().Total.Amount() != cart.Totals().Total.Amount() {
		t.Fatalf("cart = %s items=%d total=%d", got.SessionID(), len(got.Items()), got.Totals().Total.Amount())
	}
}

func TestCartRepositoryGetBySessionIDReturnsEmptyCartWhenMissing(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &CartRepository{pool: pool}

	got, err := repo.GetBySessionID(context.Background(), "missing-session")
	if err != nil {
		t.Fatalf("GetBySessionID: %v", err)
	}
	if got.SessionID() != "missing-session" || !got.IsEmpty() {
		t.Fatalf("cart = %s empty=%v, want empty missing-session cart", got.SessionID(), got.IsEmpty())
	}
}

func postgresTestOrder(t *testing.T) orderdomain.Order {
	t.Helper()
	ord, err := orderdomain.NewOrder(orderdomain.OrderInput{
		CustomerEmail: "shopper@example.com",
		Items: []orderdomain.OrderItemInput{{
			ProductID: uuid.MustParse("c1000000-0000-0000-0000-000000000001"),
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
	return orderdomain.ReconstructOrder(orderdomain.OrderRecord{
		ID:              uuid.MustParse("d1000000-0000-0000-0000-000000000001"),
		CustomerEmail:   ord.CustomerEmail(),
		Items:           ord.Items(),
		Status:          orderdomain.StatusPending,
		Totals:          ord.Totals(),
		ShippingAddress: ord.ShippingAddress(),
		CreatedAt:       time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
		UpdatedAt:       time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC),
	})
}

func postgresTestCart(t *testing.T) orderdomain.Cart {
	t.Helper()
	cart, err := orderdomain.NewCart("session-123")
	if err != nil {
		t.Fatalf("NewCart: %v", err)
	}
	if err := cart.ReplaceItems([]orderdomain.CartItemInput{{
		ProductID: uuid.MustParse("c1000000-0000-0000-0000-000000000001"),
		SKU:       "BAND-001",
		Title:     "Resistance Band",
		Quantity:  2,
		UnitPrice: mustMoney(t, 2495),
	}}); err != nil {
		t.Fatalf("ReplaceItems: %v", err)
	}
	return cart
}

func fakeOrderRow(ord orderdomain.Order) fakeRow {
	address := ord.ShippingAddress()
	totals := ord.Totals()
	return fakeRow{values: []any{
		ord.ID(),
		ord.CustomerEmail(),
		ord.Status().String(),
		totals.Subtotal.Amount(),
		totals.Subtotal.Currency(),
		totals.Shipping.Amount(),
		totals.Total.Amount(),
		address.Name,
		address.Line1,
		address.Line2,
		address.City,
		address.Region,
		address.PostalCode,
		address.Country,
		ord.CreatedAt(),
		ord.UpdatedAt(),
	}}
}

func fakeOrderItemValues(item orderdomain.OrderItem) []any {
	return []any{
		item.ProductID(),
		item.SKU(),
		item.Title(),
		item.Quantity(),
		item.UnitPrice().Amount(),
		item.UnitPrice().Currency(),
		item.LineTotal().Amount(),
	}
}

func fakeCartRow(cart orderdomain.Cart) fakeRow {
	totals := cart.Totals()
	return fakeRow{values: []any{
		cart.SessionID(),
		totals.Subtotal.Amount(),
		totals.Subtotal.Currency(),
		totals.Total.Amount(),
		cart.UpdatedAt(),
	}}
}

func fakeCartItemValues(item orderdomain.CartItem) []any {
	return []any{
		item.ProductID(),
		item.SKU(),
		item.Title(),
		item.Quantity(),
		item.UnitPrice().Amount(),
		item.UnitPrice().Currency(),
		item.LineTotal().Amount(),
	}
}

func mustMoney(t *testing.T, amount int) catalog.Money {
	t.Helper()
	money, err := catalog.NewMoney(amount, "AUD")
	if err != nil {
		t.Fatalf("NewMoney: %v", err)
	}
	return money
}
