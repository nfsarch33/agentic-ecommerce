package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/domain/digital"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

func newDigitalProductForTest(t *testing.T) digital.DigitalProduct {
	t.Helper()
	p, err := digital.NewDigitalProduct(digital.DigitalProductInput{
		TenantID: "tenant-a",
		SKU:      "PDF-001",
		Name:     "Sample",
		FilePath: "tenant-a/x.pdf",
		FileSize: 1024,
		Version:  "1",
		Now:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatalf("NewDigitalProduct: %v", err)
	}
	return p
}

func fakeDigitalProductRow(p digital.DigitalProduct) fakeRow {
	return fakeRow{values: []any{
		p.ID(),
		p.TenantID(),
		p.SKU(),
		p.Name(),
		p.Description(),
		p.FilePath(),
		int64(p.FileSize()),
		p.ContentType(),
		p.Checksum(),
		p.Version(),
		p.CreatedAt(),
		p.UpdatedAt(),
	}}
}

func TestDigitalProductRepoCreateRejectsForeignTenant(t *testing.T) {
	t.Parallel()
	repo := &DigitalProductRepository{pool: &fakePool{}}
	if err := repo.Create(context.Background(), "tenant-b", newDigitalProductForTest(t)); !errors.Is(err, digital.ErrTenantMismatch) {
		t.Fatalf("err = %v, want ErrTenantMismatch", err)
	}
}

func TestDigitalProductRepoCreateInsertsRow(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &DigitalProductRepository{pool: pool}
	if err := repo.Create(context.Background(), "tenant-a", newDigitalProductForTest(t)); err != nil {
		t.Fatalf("Create: %v", err)
	}
	if len(pool.execSQL) != 1 {
		t.Fatalf("exec count = %d", len(pool.execSQL))
	}
}

func TestDigitalProductRepoUpdateNotFoundOnZeroRows(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &DigitalProductRepository{pool: pool}
	if err := repo.Update(context.Background(), "tenant-a", newDigitalProductForTest(t)); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDigitalProductRepoUpdateRejectsForeignTenant(t *testing.T) {
	t.Parallel()
	repo := &DigitalProductRepository{pool: &fakePool{}}
	if err := repo.Update(context.Background(), "tenant-b", newDigitalProductForTest(t)); !errors.Is(err, digital.ErrTenantMismatch) {
		t.Fatalf("err = %v, want ErrTenantMismatch", err)
	}
}

func TestDigitalProductRepoGetScansRow(t *testing.T) {
	t.Parallel()
	p := newDigitalProductForTest(t)
	pool := &fakePool{row: fakeDigitalProductRow(p)}
	repo := &DigitalProductRepository{pool: pool}
	got, err := repo.Get(context.Background(), "tenant-a", p.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != p.ID() {
		t.Fatalf("got id = %v, want %v", got.ID(), p.ID())
	}
}

func TestDigitalProductRepoGetReturnsNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &DigitalProductRepository{pool: pool}
	if _, err := repo.Get(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestDigitalProductRepoListEmits(t *testing.T) {
	t.Parallel()
	p := newDigitalProductForTest(t)
	pool := &fakePool{
		rows: &fakeRows{rows: [][]any{fakeDigitalProductRow(p).values}},
		row:  fakeRow{values: []any{1}},
	}
	repo := &DigitalProductRepository{pool: pool}
	got, err := repo.List(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.Total != 1 || len(got.Products) != 1 {
		t.Fatalf("list = %+v", got)
	}
}

func TestDigitalProductRepoDelete(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("DELETE 1")}
	repo := &DigitalProductRepository{pool: pool}
	if err := repo.Delete(context.Background(), "tenant-a", uuid.New()); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	pool.commandTag = pgconn.NewCommandTag("DELETE 0")
	if err := repo.Delete(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrDigitalProductNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// LicenseRepository fake-store coverage.

func newLicenseForTest(t *testing.T) digital.License {
	t.Helper()
	lic, err := digital.NewLicense(digital.LicenseInput{
		TenantID:   "tenant-a",
		ProductID:  uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		CustomerID: uuid.MustParse("22222222-2222-2222-2222-222222222222"),
		Key:        "AAAAA-BBBBB-CCCCC-DDDDD-EEEEEEEE",
	})
	if err != nil {
		t.Fatalf("NewLicense: %v", err)
	}
	return lic
}

func fakeLicenseRow(lic digital.License) fakeRow {
	var expires *time.Time
	if !lic.ExpiresAt().IsZero() {
		t := lic.ExpiresAt()
		expires = &t
	}
	return fakeRow{values: []any{
		lic.ID(), lic.TenantID(), lic.ProductID(), lic.CustomerID(),
		lic.Key(), string(lic.State()), lic.IssuedAt(), expires,
		lic.MaxActivations(), lic.UpdatedAt(),
	}}
}

func TestLicenseRepoCreateAndGet(t *testing.T) {
	t.Parallel()
	lic := newLicenseForTest(t)
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1"), row: fakeLicenseRow(lic)}
	repo := &LicenseRepository{pool: pool}
	if err := repo.Create(context.Background(), "tenant-a", lic); err != nil {
		t.Fatalf("Create: %v", err)
	}
	got, err := repo.Get(context.Background(), "tenant-a", lic.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != lic.ID() {
		t.Fatalf("ids differ")
	}
}

func TestLicenseRepoSaveStateNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &LicenseRepository{pool: pool}
	if err := repo.SaveState(context.Background(), "tenant-a", newLicenseForTest(t)); !errors.Is(err, port.ErrLicenseNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestLicenseRepoSaveStateRejectsForeignTenant(t *testing.T) {
	t.Parallel()
	repo := &LicenseRepository{pool: &fakePool{}}
	if err := repo.SaveState(context.Background(), "tenant-b", newLicenseForTest(t)); !errors.Is(err, digital.ErrTenantMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestLicenseRepoListAndListByCustomer(t *testing.T) {
	t.Parallel()
	lic := newLicenseForTest(t)
	pool := &fakePool{
		rows: &fakeRows{rows: [][]any{fakeLicenseRow(lic).values}},
		row:  fakeRow{values: []any{1}},
	}
	repo := &LicenseRepository{pool: pool}
	got, err := repo.List(context.Background(), "tenant-a", 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("total = %d", got.Total)
	}

	pool2 := &fakePool{
		rows: &fakeRows{rows: [][]any{fakeLicenseRow(lic).values}},
		row:  fakeRow{values: []any{1}},
	}
	repo2 := &LicenseRepository{pool: pool2}
	got2, err := repo2.ListByCustomer(context.Background(), "tenant-a", lic.CustomerID(), 1, 10)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if got2.Total != 1 {
		t.Fatalf("total = %d", got2.Total)
	}
}

// AccessGrantRepository fake-store coverage.

func newGrantForTest(t *testing.T) digital.AccessGrant {
	t.Helper()
	g, err := digital.NewAccessGrant(digital.AccessGrantInput{
		TenantID:   "tenant-a",
		CustomerID: uuid.MustParse("33333333-3333-3333-3333-333333333333"),
		ProductID:  uuid.MustParse("44444444-4444-4444-4444-444444444444"),
		LicenseID:  uuid.MustParse("55555555-5555-5555-5555-555555555555"),
		Source:     digital.SourcePurchase,
	})
	if err != nil {
		t.Fatalf("NewAccessGrant: %v", err)
	}
	return g
}

func fakeAccessGrantRow(g digital.AccessGrant) fakeRow {
	return fakeRow{values: []any{
		g.ID(), g.TenantID(), g.CustomerID(), g.ProductID(),
		g.LicenseID(), g.GrantedAt(), string(g.Source()),
	}}
}

func TestAccessGrantRepoUpsertAndGet(t *testing.T) {
	t.Parallel()
	g := newGrantForTest(t)
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1"), row: fakeAccessGrantRow(g)}
	repo := &AccessGrantRepository{pool: pool}
	if err := repo.Upsert(context.Background(), "tenant-a", g); err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	got, err := repo.Get(context.Background(), "tenant-a", g.ID())
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.ID() != g.ID() {
		t.Fatalf("ids differ")
	}
}

func TestAccessGrantRepoUpsertRejectsForeignTenant(t *testing.T) {
	t.Parallel()
	repo := &AccessGrantRepository{pool: &fakePool{}}
	if err := repo.Upsert(context.Background(), "tenant-b", newGrantForTest(t)); !errors.Is(err, digital.ErrTenantMismatch) {
		t.Fatalf("err = %v", err)
	}
}

func TestAccessGrantRepoGetByCustomerProduct(t *testing.T) {
	t.Parallel()
	g := newGrantForTest(t)
	pool := &fakePool{row: fakeAccessGrantRow(g)}
	repo := &AccessGrantRepository{pool: pool}
	got, err := repo.GetByCustomerProduct(context.Background(), "tenant-a", g.CustomerID(), g.ProductID())
	if err != nil {
		t.Fatalf("GetByCustomerProduct: %v", err)
	}
	if got.ID() != g.ID() {
		t.Fatalf("ids differ")
	}
}

func TestAccessGrantRepoListByCustomer(t *testing.T) {
	t.Parallel()
	g := newGrantForTest(t)
	pool := &fakePool{
		rows: &fakeRows{rows: [][]any{fakeAccessGrantRow(g).values}},
		row:  fakeRow{values: []any{1}},
	}
	repo := &AccessGrantRepository{pool: pool}
	got, err := repo.ListByCustomer(context.Background(), "tenant-a", g.CustomerID(), 1, 10)
	if err != nil {
		t.Fatalf("ListByCustomer: %v", err)
	}
	if got.Total != 1 {
		t.Fatalf("total = %d", got.Total)
	}
}

func TestAccessGrantRepoGetReturnsNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &AccessGrantRepository{pool: pool}
	if _, err := repo.Get(context.Background(), "tenant-a", uuid.New()); !errors.Is(err, port.ErrAccessGrantNotFound) {
		t.Fatalf("err = %v", err)
	}
}
