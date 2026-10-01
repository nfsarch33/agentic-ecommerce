package postgres

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
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
	if p.rows == nil && p.queryErr == nil {
		// an empty media result set: a product with no image rows is a
		// legitimate answer, distinct from a pool that errors
		return &fakeRows{}, nil
	}
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
	case *int64:
		switch v := value.(type) {
		case int64:
			*d = v
		case int:
			*d = int64(v)
		default:
			panic("unsupported int64 scan value")
		}
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
	case *[]byte:
		if value == nil {
			*d = nil
			return
		}
		switch v := value.(type) {
		case []byte:
			out := make([]byte, len(v))
			copy(out, v)
			*d = out
		case string:
			*d = []byte(v)
		default:
			panic("unsupported []byte scan value")
		}
	case *bool:
		if value == nil {
			*d = false
			return
		}
		b, ok := value.(bool)
		if !ok {
			panic("unsupported bool scan value")
		}
		*d = b
	default:
		panic("unsupported scan destination")
	}
}

// TestGetByIDHydratesImages is the live-PG integration pin: a product with
// media-asset rows must come back carrying its images (with sort order),
// or the publish workflow's compliance gate fails it no matter what was
// seeded. The always-running shape test lives in
// TestGetByIDHydratesImagesFromFake below.
// Mutant: dropping the loadImages call in getOne fails this test.
func TestGetByIDHydratesImages(t *testing.T) {
	ctx := context.Background()
	dsn := os.Getenv("ECOMMERCE_TEST_PG_DSN")
	if dsn == "" {
		t.Skip("ECOMMERCE_TEST_PG_DSN unset — the hydration pin needs a live schema (CI supplies one; a hardcoded local fallback hides its absence)")
	}
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Skipf("pg dsn unusable: %v", err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(ctx); err != nil {
		t.Skipf("pg not reachable: %v", err)
	}
	repo := NewProductRepository(pool)
	pid := uuid.MustParse("00000000-0000-0000-0000-00000000beef")
	sku := "IMG-HYD-" + fmt.Sprint(time.Now().UnixNano()%100000)
	_, perr := pool.Exec(ctx, `INSERT INTO products (id, sku, title, slug, description, price_amount, stock, status)
		VALUES ($1,$2,'Hydration probe','hydration-probe','desc',100,1,'draft') ON CONFLICT (id) DO NOTHING`, pid, sku)
	if perr != nil {
		t.Fatalf("seed product: %v", perr)
	}
	t.Cleanup(func() {
		if _, err := pool.Exec(ctx, `DELETE FROM product_media_assets WHERE product_id=$1`, pid); err != nil {
			t.Logf("cleanup media rows: %v", err)
		}
		if _, err := pool.Exec(ctx, `DELETE FROM products WHERE id=$1`, pid); err != nil {
			t.Logf("cleanup product row: %v", err)
		}
	})
	pool.Exec(ctx, `DELETE FROM product_media_assets WHERE product_id=$1`, pid)
	_, aerr := pool.Exec(ctx, `INSERT INTO product_media_assets
		(product_id, storage_key, source_url, public_url, original_filename, mime_type, size_bytes, width_px, height_px, alt_text, sort_order)
		VALUES ($1,'imgprobe/'||$2||'-1.jpg','https://example.com/1.jpg','https://example.com/1.jpg','1.jpg','image/jpeg',4200,600,400,'Probe image one',0),
		       ($1,'imgprobe/'||$2||'-2.jpg','https://example.com/2.jpg','https://example.com/2.jpg','2.jpg','image/jpeg',4200,600,400,'Probe image two',1)`, pid, sku)
	if aerr != nil {
		t.Fatalf("seed asset: %v", aerr)
	}
	p, err := repo.GetByID(ctx, pid)
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	got := p.Images()
	if len(got) != 2 {
		t.Fatalf("images = %d, want 2 (the publish gate requires them)", len(got))
	}
	if got[0].URL != "https://example.com/1.jpg" || got[0].Alt != "Probe image one" {
		t.Fatalf("first image = %+v", got[0])
	}
	if got[1].SortOrder != 1 || got[1].Alt != "Probe image two" {
		t.Fatalf("second image = %+v (want SortOrder 1 and the alt text)", got[1])
	}
}

// TestGetByIDSurfacesImageLoadError pins review round 2 of #219: a pool that
// errors on the media query must fail the read, not answer with a silent
// zero-image product. Mutant: swallowing the loadImages error again makes
// this test fail (the product comes back with nil error).
func TestGetByIDSurfacesImageLoadError(t *testing.T) {
	product := postgresTestProduct(t)
	pool := &fakePool{row: fakeProductRow(product), queryErr: errors.New("media table unavailable")}
	repo := &ProductRepository{pool: pool}
	if _, err := repo.GetByID(context.Background(), product.ID()); err == nil {
		t.Fatal("GetByID must surface the image-load error; got nil (the swallow is back)")
	} else if !strings.Contains(err.Error(), "media table unavailable") {
		t.Fatalf("err = %v; want the loadImages error to propagate", err)
	}
}

// TestGetByIDHydratesImagesFromFake runs EVERYWHERE (no DSN, no skip): the
// fake pool serves a product row plus a two-row media result set, and
// GetByID must return both images with URL, alt text and sort order —
// the shape the compliance gate's image rule reads. Mutant: keeping the
// loadImages call but returning the un-hydrated product fails here with
// images = 0 even though every live gate stays green.
func TestGetByIDHydratesImagesFromFake(t *testing.T) {
	t.Parallel()
	product := postgresTestProduct(t)
	pool := &fakePool{
		row: fakeProductRow(product),
		rows: &fakeRows{rows: [][]any{
			{"https://example.com/one.jpg", "First image", 0},
			{"https://example.com/two.jpg", "Second image", 1},
		}},
	}
	repo := &ProductRepository{pool: pool}
	got, err := repo.GetByID(context.Background(), product.ID())
	if err != nil {
		t.Fatalf("GetByID: %v", err)
	}
	imgs := got.Images()
	if len(imgs) != 2 {
		t.Fatalf("images = %d, want 2 (the compliance image rule reads this)", len(imgs))
	}
	if imgs[0].URL != "https://example.com/one.jpg" || imgs[0].Alt != "First image" || imgs[0].SortOrder != 0 {
		t.Fatalf("first image = %+v", imgs[0])
	}
	if imgs[1].URL != "https://example.com/two.jpg" || imgs[1].Alt != "Second image" || imgs[1].SortOrder != 1 {
		t.Fatalf("second image = %+v (want URL/alt/sort order)", imgs[1])
	}
}
