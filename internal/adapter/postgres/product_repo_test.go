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
)

func TestProductRepositoryCreateExecsInsert(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &ProductRepository{pool: pool}
	product := postgresTestProduct(t)

	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(pool.execSQL) != 1 {
		t.Fatalf("exec calls = %d, want 1", len(pool.execSQL))
	}
}

func TestProductRepositoryGetBySlugScansProduct(t *testing.T) {
	t.Parallel()
	product := postgresTestProduct(t)
	pool := &fakePool{row: fakeProductRow(product)}
	repo := &ProductRepository{pool: pool}

	got, err := repo.GetBySlug(context.Background(), product.Slug())
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID() != product.ID() || got.SKU() != product.SKU() {
		t.Fatalf("product = %s/%s, want %s/%s", got.ID(), got.SKU(), product.ID(), product.SKU())
	}
}

func TestProductRepositoryListScansProductsAndTotal(t *testing.T) {
	t.Parallel()
	product := postgresTestProduct(t)
	pool := &fakePool{
		row:  fakeRow{values: []any{1}},
		rows: &fakeRows{rows: [][]any{fakeProductValues(product)}},
	}
	repo := &ProductRepository{pool: pool}

	got, err := repo.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.Total != 1 || len(got.Products) != 1 {
		t.Fatalf("list = total %d len %d, want one product", got.Total, len(got.Products))
	}
	if got.Products[0].Slug() != product.Slug() {
		t.Fatalf("slug = %q, want %q", got.Products[0].Slug(), product.Slug())
	}
}

func TestProductRepositoryUpdateReturnsNotFound(t *testing.T) {
	t.Parallel()
	repo := &ProductRepository{pool: &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}}

	if err := repo.Update(context.Background(), postgresTestProduct(t)); !errors.Is(err, ErrProductNotFound) {
		t.Fatalf("Update err = %v, want ErrProductNotFound", err)
	}
}

func TestProductRepositoryDeleteWrapsExecError(t *testing.T) {
	t.Parallel()
	want := errors.New("boom")
	repo := &ProductRepository{pool: &fakePool{execErr: want}}

	err := repo.Delete(context.Background(), uuid.New())
	if !errors.Is(err, want) {
		t.Fatalf("Delete err = %v, want wrapped %v", err, want)
	}
}

func postgresTestProduct(t *testing.T) catalog.Product {
	t.Helper()
	price, err := catalog.NewMoney(4995, "AUD")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	now := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          uuid.MustParse("b1000000-0000-0000-0000-000000000001"),
		SKU:         "RB-SET-5",
		Title:       "Resistance Band Set",
		Slug:        "resistance-band-set",
		Description: "Progressive resistance band set.",
		Price:       price,
		Stock:       120,
		Status:      catalog.StatusActive,
		CreatedAt:   now,
		UpdatedAt:   now,
	})
}

type fakePool struct {
	execSQL    []string
	querySQL   []string
	commandTag pgconn.CommandTag
	execErr    error
	queryErr   error
	row        fakeRow
	rows       pgx.Rows
}

func (p *fakePool) Exec(_ context.Context, sql string, _ ...any) (pgconn.CommandTag, error) {
	p.execSQL = append(p.execSQL, sql)
	return p.commandTag, p.execErr
}

func (p *fakePool) QueryRow(_ context.Context, sql string, _ ...any) pgx.Row {
	p.querySQL = append(p.querySQL, sql)
	return p.row
}

func (p *fakePool) Query(_ context.Context, sql string, _ ...any) (pgx.Rows, error) {
	p.querySQL = append(p.querySQL, sql)
	return p.rows, p.queryErr
}

type fakeRow struct {
	values []any
	err    error
}

func fakeProductRow(product catalog.Product) fakeRow {
	return fakeRow{values: fakeProductValues(product)}
}

func fakeProductValues(product catalog.Product) []any {
	return []any{
		product.ID(),
		product.SKU(),
		product.Title(),
		product.Slug(),
		product.Description(),
		product.Price().Amount(),
		product.Price().Currency(),
		product.Stock(),
		product.Status().String(),
		product.CreatedAt(),
		product.UpdatedAt(),
	}
}

func (r fakeRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		assignScanValue(dest[i], r.values[i])
	}
	return nil
}

type fakeRows struct {
	rows   [][]any
	index  int
	closed bool
	err    error
}

func (r *fakeRows) Close()                                       { r.closed = true }
func (r *fakeRows) Err() error                                   { return r.err }
func (r *fakeRows) CommandTag() pgconn.CommandTag                { return pgconn.NewCommandTag("SELECT 1") }
func (r *fakeRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeRows) Values() ([]any, error)                       { return r.rows[r.index-1], nil }
func (r *fakeRows) RawValues() [][]byte                          { return nil }
func (r *fakeRows) Conn() *pgx.Conn                              { return nil }

func (r *fakeRows) Next() bool {
	if r.index >= len(r.rows) {
		return false
	}
	r.index++
	return true
}

func (r *fakeRows) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	for i := range dest {
		assignScanValue(dest[i], r.rows[r.index-1][i])
	}
	return nil
}

func assignScanValue(dest, value any) {
	switch d := dest.(type) {
	case *uuid.UUID:
		*d = value.(uuid.UUID)
	case *string:
		*d = value.(string)
	case *int:
		*d = value.(int)
	case *time.Time:
		*d = value.(time.Time)
	case *[]string:
		if value == nil {
			*d = nil
			return
		}
		strs, ok := value.([]string)
		if !ok {
			panic("unsupported []string scan value")
		}
		// Defensive copy: callers should not see aliased state.
		out := make([]string, len(strs))
		copy(out, strs)
		*d = out
	case **time.Time:
		if value == nil {
			*d = nil
			return
		}
		switch v := value.(type) {
		case *time.Time:
			*d = v
		case time.Time:
			t := v
			*d = &t
		default:
			panic("unsupported *time.Time scan value")
		}
	default:
		panic("unsupported scan destination")
	}
}
