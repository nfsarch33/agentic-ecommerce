package postgres

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	orderdomain "github.com/nfsarch33/helixon-ec/internal/domain/order"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

type mockProductStore struct {
	execCalls     []mockExecCall
	queryCalls    []mockQueryCall
	queryRowCalls []mockQueryRowCall
	execResult    pgconn.CommandTag
	execErr       error
	queryErr      error
	scanErr       error
	rows          *mockRows
	rowResult     *mockRow
}

type mockExecCall struct {
	SQL  string
	Args []any
}

type mockQueryCall struct {
	SQL  string
	Args []any
}

type mockQueryRowCall struct {
	SQL  string
	Args []any
}

func (m *mockProductStore) Exec(_ context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	m.execCalls = append(m.execCalls, mockExecCall{SQL: sql, Args: args})
	return m.execResult, m.execErr
}

func (m *mockProductStore) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	m.queryCalls = append(m.queryCalls, mockQueryCall{SQL: sql, Args: args})
	if m.queryErr != nil {
		return nil, m.queryErr
	}
	return m.rows, nil
}

func (m *mockProductStore) QueryRow(_ context.Context, sql string, args ...any) pgx.Row {
	m.queryRowCalls = append(m.queryRowCalls, mockQueryRowCall{SQL: sql, Args: args})
	return m.rowResult
}

type mockRow struct {
	scanFn func(dest ...any) error
}

func (r *mockRow) Scan(dest ...any) error {
	if r.scanFn != nil {
		return r.scanFn(dest...)
	}
	return nil
}

type mockRows struct {
	closed bool
	items  []mockRowData
	index  int
	err    error
}

type mockRowData struct {
	id            uuid.UUID
	sku, title    string
	slug, desc    string
	priceAmount   int
	priceCurrency string
	stock         int
	status        string
	createdAt     time.Time
	updatedAt     time.Time
}

func (r *mockRows) Next() bool {
	if r.index >= len(r.items) {
		return false
	}
	r.index++
	return true
}

func (r *mockRows) Scan(dest ...any) error {
	item := r.items[r.index-1]
	*dest[0].(*uuid.UUID) = item.id
	*dest[1].(*string) = item.sku
	*dest[2].(*string) = item.title
	*dest[3].(*string) = item.slug
	*dest[4].(*string) = item.desc
	*dest[5].(*int) = item.priceAmount
	*dest[6].(*string) = item.priceCurrency
	*dest[7].(*int) = item.stock
	*dest[8].(*string) = item.status
	*dest[9].(*time.Time) = item.createdAt
	*dest[10].(*time.Time) = item.updatedAt
	return nil
}

func (r *mockRows) Close()                                       { r.closed = true }
func (r *mockRows) Err() error                                   { return r.err }
func (r *mockRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *mockRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *mockRows) RawValues() [][]byte                          { return nil }
func (r *mockRows) Values() ([]any, error)                       { return nil, nil }
func (r *mockRows) Conn() *pgx.Conn                              { return nil }

func TestProductRepository_CreateWithTenant(t *testing.T) {
	store := &mockProductStore{}
	repo := &ProductRepository{pool: store}

	price, _ := catalog.NewMoney(1999, "AUD")
	product, _ := catalog.NewProduct(catalog.ProductInput{
		SKU:    "TEST-SKU",
		Title:  "Test Product",
		Slug:   "test-product",
		Price:  price,
		Stock:  10,
		Status: catalog.StatusDraft,
	})

	err := repo.CreateWithTenant(context.Background(), product, "shop-xyz")
	if err != nil {
		t.Fatalf("CreateWithTenant: %v", err)
	}

	if len(store.execCalls) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(store.execCalls))
	}
	call := store.execCalls[0]
	if len(call.Args) != 12 {
		t.Fatalf("args count = %d, want 12", len(call.Args))
	}
	tenantArg, ok := call.Args[9].(string)
	if !ok || tenantArg != "shop-xyz" {
		t.Errorf("tenant_id arg = %v, want shop-xyz", call.Args[9])
	}
}

func TestProductRepository_CreateWithTenantRejectsMissingTenant(t *testing.T) {
	store := &mockProductStore{}
	repo := &ProductRepository{pool: store}
	product := testTenantProduct(t)

	err := repo.CreateWithTenant(context.Background(), product, " ")
	if err == nil {
		t.Fatal("CreateWithTenant accepted a missing tenant")
	}
	if len(store.execCalls) != 0 {
		t.Fatalf("exec calls = %d, want 0 when tenant is missing", len(store.execCalls))
	}
}

func TestProductRepository_ListByTenant(t *testing.T) {
	id1 := uuid.New()
	now := time.Now().UTC()
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(dest ...any) error {
			*dest[0].(*int) = 1
			return nil
		}},
		rows: &mockRows{
			items: []mockRowData{
				{id: id1, sku: "SKU-1", title: "P1", slug: "p1", desc: "", priceAmount: 500, priceCurrency: "AUD", stock: 3, status: "active", createdAt: now, updatedAt: now},
			},
		},
	}
	repo := &ProductRepository{pool: store}

	result, err := repo.ListByTenant(context.Background(), "tenant-abc", 1, 20)
	if err != nil {
		t.Fatalf("ListByTenant: %v", err)
	}

	if result.Total != 1 {
		t.Errorf("total = %d, want 1", result.Total)
	}
	if len(result.Products) != 1 {
		t.Errorf("products = %d, want 1", len(result.Products))
	}

	if len(store.queryRowCalls) != 1 {
		t.Fatalf("query_row calls = %d, want 1", len(store.queryRowCalls))
	}
	countCall := store.queryRowCalls[0]
	if countCall.Args[0] != "tenant-abc" {
		t.Errorf("count query tenant = %v, want tenant-abc", countCall.Args[0])
	}

	if len(store.queryCalls) != 1 {
		t.Fatalf("query calls = %d, want 1", len(store.queryCalls))
	}
	listCall := store.queryCalls[0]
	if listCall.Args[0] != "tenant-abc" {
		t.Errorf("list query tenant = %v, want tenant-abc", listCall.Args[0])
	}
}

func TestProductRepository_ListByTenantRejectsMissingTenant(t *testing.T) {
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error {
			t.Fatal("ListByTenant should fail before querying when tenant is missing")
			return nil
		}},
	}
	repo := &ProductRepository{pool: store}

	_, err := repo.ListByTenant(context.Background(), "", 1, 20)
	if err == nil {
		t.Fatal("ListByTenant accepted a missing tenant")
	}
	if len(store.queryRowCalls) != 0 || len(store.queryCalls) != 0 {
		t.Fatalf("query calls = row:%d list:%d, want none", len(store.queryRowCalls), len(store.queryCalls))
	}
}

func TestProductRepository_GetByIDAndTenant_NotFound(t *testing.T) {
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error {
			return pgx.ErrNoRows
		}},
	}
	repo := &ProductRepository{pool: store}

	_, err := repo.GetByIDAndTenant(context.Background(), uuid.New(), "tenant-x")
	if err != ErrProductNotFound {
		t.Errorf("GetByIDAndTenant = %v, want ErrProductNotFound", err)
	}
}

func TestProductRepository_GetByIDAndTenantRejectsMissingTenant(t *testing.T) {
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error {
			t.Fatal("GetByIDAndTenant should fail before querying when tenant is missing")
			return nil
		}},
	}
	repo := &ProductRepository{pool: store}

	_, err := repo.GetByIDAndTenant(context.Background(), uuid.New(), "")
	if err == nil {
		t.Fatal("GetByIDAndTenant accepted a missing tenant")
	}
	if len(store.queryRowCalls) != 0 {
		t.Fatalf("query row calls = %d, want 0", len(store.queryRowCalls))
	}
}

func TestOrderRepository_CreateWithTenantRejectsMissingTenant(t *testing.T) {
	store := &mockProductStore{}
	repo := &OrderRepository{pool: store}
	order := testTenantOrder(t)

	err := repo.CreateWithTenant(context.Background(), order, "")
	if err == nil {
		t.Fatal("CreateWithTenant accepted a missing tenant")
	}
	if len(store.execCalls) != 0 {
		t.Fatalf("exec calls = %d, want 0 when tenant is missing", len(store.execCalls))
	}
}

func TestTenantProductRepository_Interface(t *testing.T) {
	store := &mockProductStore{}
	repo := &ProductRepository{pool: store}
	var _ port.TenantProductRepository = repo
}

func TestTenantOrderRepository_Interface(t *testing.T) {
	store := &mockProductStore{}
	repo := &OrderRepository{pool: store}
	var _ port.TenantOrderRepository = repo
}

func testTenantProduct(t *testing.T) catalog.Product {
	t.Helper()
	price, _ := catalog.NewMoney(1999, "AUD")
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:    "TEST-SKU",
		Title:  "Test Product",
		Slug:   "test-product",
		Price:  price,
		Stock:  10,
		Status: catalog.StatusDraft,
	})
	if err != nil {
		t.Fatalf("new product: %v", err)
	}
	return product
}

func testTenantOrder(t *testing.T) orderdomain.Order {
	t.Helper()
	price, _ := catalog.NewMoney(1000, "AUD")
	order, err := orderdomain.NewOrder(orderdomain.OrderInput{
		CustomerEmail: "shopper@example.com",
		Items: []orderdomain.OrderItemInput{
			{ProductID: uuid.New(), SKU: "SKU-1", Title: "Product", Quantity: 1, UnitPrice: price},
		},
		ShippingAddress: orderdomain.ShippingAddress{
			Name:       "Jane Shopper",
			Line1:      "1 Market Street",
			City:       "Sydney",
			Region:     "NSW",
			PostalCode: "2000",
			Country:    "AU",
		},
	})
	if err != nil {
		t.Fatalf("new order: %v", err)
	}
	return order
}
