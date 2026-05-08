package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/order"
)

// File scope: extra unit coverage for previously-uncovered branches in the
// product/order/cart repositories. All tests use the shared mockProductStore
// from tenant_repo_test.go so the suite runs with no Docker dependency and
// no shared database, while still exercising error wrapping, tenant
// guards, and the not-found mapping that production callers rely on.

func TestProductRepositoryGetByIDMapsNotFound(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error { return pgx.ErrNoRows }},
	}
	repo := &ProductRepository{pool: store}

	if _, err := repo.GetByID(context.Background(), uuid.New()); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("GetByID err = %v, want ErrProductNotFound", err)
	}
	if len(store.queryRowCalls) != 1 {
		t.Fatalf("queryRow calls = %d, want 1", len(store.queryRowCalls))
	}
}

func TestProductRepositoryGetByIDReturnsScanError(t *testing.T) {
	t.Parallel()

	want := errors.New("scan boom")
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error { return want }},
	}
	repo := &ProductRepository{pool: store}

	_, err := repo.GetByID(context.Background(), uuid.New())
	if !errors.Is(err, want) {
		t.Fatalf("GetByID err = %v, want wrapped %v", err, want)
	}
}

func TestProductRepositoryDeleteMapsNotFoundForZeroRows(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{execResult: pgconn.NewCommandTag("DELETE 0")}
	repo := &ProductRepository{pool: store}

	if err := repo.Delete(context.Background(), uuid.New()); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("Delete err = %v, want ErrProductNotFound", err)
	}
}

func TestProductRepositoryUpdateWrapsExecError(t *testing.T) {
	t.Parallel()

	want := errors.New("update boom")
	store := &mockProductStore{execErr: want}
	repo := &ProductRepository{pool: store}

	err := repo.Update(context.Background(), postgresTestProduct(t))
	if !errors.Is(err, want) {
		t.Fatalf("Update err = %v, want wrapped %v", err, want)
	}
}

func TestProductRepositoryListWrapsCountError(t *testing.T) {
	t.Parallel()

	want := errors.New("count boom")
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error { return want }},
	}
	repo := &ProductRepository{pool: store}

	if _, err := repo.List(context.Background(), 1, 10); !errors.Is(err, want) {
		t.Fatalf("List err = %v, want wrapped %v", err, want)
	}
}

func TestProductRepositoryListWrapsQueryError(t *testing.T) {
	t.Parallel()

	want := errors.New("query boom")
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*int) = 0
			return nil
		}},
		queryErr: want,
	}
	repo := &ProductRepository{pool: store}

	if _, err := repo.List(context.Background(), 1, 10); !errors.Is(err, want) {
		t.Fatalf("List err = %v, want wrapped %v", err, want)
	}
}

func TestProductRepositoryListByTenantWrapsCountError(t *testing.T) {
	t.Parallel()

	want := errors.New("count tenant boom")
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error { return want }},
	}
	repo := &ProductRepository{pool: store}

	if _, err := repo.ListByTenant(context.Background(), "tenant-x", 1, 10); !errors.Is(err, want) {
		t.Fatalf("ListByTenant err = %v, want wrapped %v", err, want)
	}
}

func TestProductRepositoryGetByIDAndTenantSuccess(t *testing.T) {
	t.Parallel()

	product := postgresTestProduct(t)
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: scanProductValuesInto(product)},
	}
	repo := &ProductRepository{pool: store}

	got, err := repo.GetByIDAndTenant(context.Background(), product.ID(), "tenant-a")
	if err != nil {
		t.Fatalf("GetByIDAndTenant: %v", err)
	}
	if got.ID() != product.ID() {
		t.Fatalf("ID = %s, want %s", got.ID(), product.ID())
	}
	if len(store.queryRowCalls) != 1 || store.queryRowCalls[0].Args[1] != "tenant-a" {
		t.Fatalf("query args = %#v", store.queryRowCalls)
	}
}

func TestOrderRepositoryGetByIDAndTenantReadsOrderWithItems(t *testing.T) {
	t.Parallel()

	order := postgresTestOrder(t)
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: scanOrderValuesInto(order)},
		rows:      &mockRows{},
	}
	repo := &OrderRepository{pool: store}

	got, err := repo.GetByIDAndTenant(context.Background(), order.ID(), "tenant-a")
	if err != nil {
		t.Fatalf("GetByIDAndTenant: %v", err)
	}
	if got.ID() != order.ID() {
		t.Fatalf("ID = %s, want %s", got.ID(), order.ID())
	}
	if len(store.queryRowCalls) != 1 {
		t.Fatalf("queryRow calls = %d, want 1", len(store.queryRowCalls))
	}
	tenantArg, ok := store.queryRowCalls[0].Args[1].(string)
	if !ok || tenantArg != "tenant-a" {
		t.Fatalf("tenant arg = %v, want tenant-a", store.queryRowCalls[0].Args[1])
	}
}

func TestOrderRepositoryGetByIDAndTenantMapsNotFound(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error { return pgx.ErrNoRows }},
	}
	repo := &OrderRepository{pool: store}

	_, err := repo.GetByIDAndTenant(context.Background(), uuid.New(), "tenant-a")
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

func TestOrderRepositoryGetByIDAndTenantRejectsMissingTenant(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{}
	repo := &OrderRepository{pool: store}

	_, err := repo.GetByIDAndTenant(context.Background(), uuid.New(), "")
	if !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("err = %v, want ErrTenantRequired", err)
	}
	if len(store.queryRowCalls) != 0 {
		t.Fatalf("queryRow calls = %d, want 0", len(store.queryRowCalls))
	}
}

func TestOrderRepositoryUpdateStatusWithTenantPersistsTransition(t *testing.T) {
	t.Parallel()

	order := postgresTestOrder(t)
	store := &mockProductStore{
		rowResult:  &mockRow{scanFn: scanOrderValuesInto(order)},
		rows:       &mockRows{},
		execResult: pgconn.NewCommandTag("UPDATE 1"),
	}
	repo := &OrderRepository{pool: store}

	got, err := repo.UpdateStatusWithTenant(context.Background(), order.ID(), orderdomain.StatusPaid, "tenant-a")
	if err != nil {
		t.Fatalf("UpdateStatusWithTenant: %v", err)
	}
	if got.Status() != orderdomain.StatusPaid {
		t.Fatalf("status = %q, want paid", got.Status())
	}
	if len(store.execCalls) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(store.execCalls))
	}
	if store.execCalls[0].Args[1] != "tenant-a" {
		t.Fatalf("tenant arg = %v, want tenant-a", store.execCalls[0].Args[1])
	}
}

func TestOrderRepositoryUpdateStatusWithTenantRejectsMissingTenant(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{}
	repo := &OrderRepository{pool: store}

	_, err := repo.UpdateStatusWithTenant(context.Background(), uuid.New(), orderdomain.StatusPaid, "  ")
	if !errors.Is(err, ErrTenantRequired) {
		t.Fatalf("err = %v, want ErrTenantRequired", err)
	}
}

func TestOrderRepositoryUpdateStatusWithTenantReturnsNotFoundOnZeroRows(t *testing.T) {
	t.Parallel()

	order := postgresTestOrder(t)
	store := &mockProductStore{
		rowResult:  &mockRow{scanFn: scanOrderValuesInto(order)},
		rows:       &mockRows{},
		execResult: pgconn.NewCommandTag("UPDATE 0"),
	}
	repo := &OrderRepository{pool: store}

	_, err := repo.UpdateStatusWithTenant(context.Background(), order.ID(), orderdomain.StatusPaid, "tenant-a")
	if !errors.Is(err, ErrOrderNotFound) {
		t.Fatalf("err = %v, want ErrOrderNotFound", err)
	}
}

func TestOrderRepositoryCreateWithTenantPersistsOrderAndItems(t *testing.T) {
	t.Parallel()

	order := postgresTestOrder(t)
	store := &mockProductStore{execResult: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &OrderRepository{pool: store}

	if err := repo.CreateWithTenant(context.Background(), order, "tenant-a"); err != nil {
		t.Fatalf("CreateWithTenant: %v", err)
	}
	if len(store.execCalls) != 1+len(order.Items()) {
		t.Fatalf("exec calls = %d, want order insert plus %d item inserts", len(store.execCalls), len(order.Items()))
	}
	tenantArg, ok := store.execCalls[0].Args[14].(string)
	if !ok || tenantArg != "tenant-a" {
		t.Fatalf("tenant arg = %v, want tenant-a", store.execCalls[0].Args[14])
	}
}

func TestOrderRepositoryCreateWraps(t *testing.T) {
	t.Parallel()

	order := postgresTestOrder(t)
	want := errors.New("order insert boom")
	store := &mockProductStore{execErr: want}
	repo := &OrderRepository{pool: store}

	if err := repo.Create(context.Background(), order); !errors.Is(err, want) {
		t.Fatalf("Create err = %v, want wrapped %v", err, want)
	}
}

func TestCartRepositorySaveWrapsUpsertError(t *testing.T) {
	t.Parallel()

	want := errors.New("upsert boom")
	store := &mockProductStore{execErr: want}
	repo := &CartRepository{pool: store}

	if err := repo.Save(context.Background(), postgresTestCart(t)); !errors.Is(err, want) {
		t.Fatalf("Save err = %v, want wrapped %v", err, want)
	}
}

func TestRequireTenantIDTrimsAndRejects(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "trims surrounding whitespace", input: "   tenant-x   ", want: "tenant-x"},
		{name: "rejects empty", input: "", wantErr: true},
		{name: "rejects whitespace only", input: "  \t \n", wantErr: true},
		{name: "passes non-trimmed value through", input: "tenant-y", want: "tenant-y"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := requireTenantID(tc.input)
			if tc.wantErr {
				if !errors.Is(err, ErrTenantRequired) {
					t.Fatalf("err = %v, want ErrTenantRequired", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("err = %v, want nil", err)
			}
			if got != tc.want {
				t.Fatalf("requireTenantID(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestProductRepositoryUpdateRefreshesUpdatedAt(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{execResult: pgconn.NewCommandTag("UPDATE 1")}
	repo := &ProductRepository{pool: store}
	product := postgresTestProduct(t)
	before := time.Now().UTC().Add(-time.Hour)
	stale := catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          product.ID(),
		SKU:         product.SKU(),
		Title:       product.Title(),
		Slug:        product.Slug(),
		Description: product.Description(),
		Price:       product.Price(),
		Stock:       product.Stock(),
		Status:      product.Status(),
		CreatedAt:   product.CreatedAt(),
		UpdatedAt:   before,
	})

	if err := repo.Update(context.Background(), stale); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(store.execCalls) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(store.execCalls))
	}
	updatedAt, ok := store.execCalls[0].Args[9].(time.Time)
	if !ok {
		t.Fatalf("updated_at arg = %T", store.execCalls[0].Args[9])
	}
	if !updatedAt.After(before) {
		t.Fatalf("updated_at = %s, want > %s", updatedAt, before)
	}
}

// scanProductValuesInto returns a Scan implementation that copies a
// catalog.Product's column values into the destination targets, mirroring
// the order in scanProductRow.
func scanProductValuesInto(product catalog.Product) func(dest ...any) error {
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = product.ID()
		*dest[1].(*string) = product.SKU()
		*dest[2].(*string) = product.Title()
		*dest[3].(*string) = product.Slug()
		*dest[4].(*string) = product.Description()
		*dest[5].(*int) = product.Price().Amount()
		*dest[6].(*string) = product.Price().Currency()
		*dest[7].(*int) = product.Stock()
		*dest[8].(*string) = product.Status().String()
		*dest[9].(*time.Time) = product.CreatedAt()
		*dest[10].(*time.Time) = product.UpdatedAt()
		return nil
	}
}

// scanOrderValuesInto returns a Scan implementation that copies an
// orderdomain.Order into the destination targets, mirroring the order in
// scanOrderRow.
func scanOrderValuesInto(order orderdomain.Order) func(dest ...any) error {
	address := order.ShippingAddress()
	totals := order.Totals()
	return func(dest ...any) error {
		*dest[0].(*uuid.UUID) = order.ID()
		*dest[1].(*string) = order.CustomerEmail()
		*dest[2].(*string) = order.Status().String()
		*dest[3].(*int) = totals.Subtotal.Amount()
		*dest[4].(*string) = totals.Subtotal.Currency()
		*dest[5].(*int) = totals.Shipping.Amount()
		*dest[6].(*int) = totals.Total.Amount()
		*dest[7].(*string) = address.Name
		*dest[8].(*string) = address.Line1
		*dest[9].(*string) = address.Line2
		*dest[10].(*string) = address.City
		*dest[11].(*string) = address.Region
		*dest[12].(*string) = address.PostalCode
		*dest[13].(*string) = address.Country
		*dest[14].(*time.Time) = order.CreatedAt()
		*dest[15].(*time.Time) = order.UpdatedAt()
		return nil
	}
}
