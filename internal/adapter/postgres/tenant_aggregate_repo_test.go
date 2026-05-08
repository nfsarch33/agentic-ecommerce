package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

func fakeTenantRow(t tenant.Tenant) fakeRow {
	return fakeRow{values: []any{
		string(t.ID), t.Slug, t.Name, t.Plan, string(t.Status),
		t.CreatedAt, t.UpdatedAt,
	}}
}

func newTenantForTest() tenant.Tenant {
	now := time.Date(2026, 5, 8, 10, 0, 0, 0, time.UTC)
	return tenant.Tenant{
		ID: "acme", Slug: "acme", Name: "Acme", Plan: "free",
		Status: tenant.StatusProvisioning, CreatedAt: now, UpdatedAt: now,
	}
}

func TestTenantAggregateCreateSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("INSERT 0 1")}
	repo := &TenantAggregateRepository{pool: pool}
	if err := repo.Create(context.Background(), newTenantForTest()); err != nil {
		t.Fatalf("Create: %v", err)
	}
}

func TestTenantAggregateCreateUniqueViolation(t *testing.T) {
	t.Parallel()
	pool := &fakePool{execErr: errors.New("ERROR: 23505 duplicate key")}
	repo := &TenantAggregateRepository{pool: pool}
	err := repo.Create(context.Background(), newTenantForTest())
	if !errors.Is(err, tenant.ErrTenantSlugAlreadyExists) {
		t.Fatalf("err = %v", err)
	}
}

func TestTenantAggregateCreateRequiresID(t *testing.T) {
	t.Parallel()
	repo := &TenantAggregateRepository{pool: &fakePool{}}
	t1 := newTenantForTest()
	t1.ID = ""
	if err := repo.Create(context.Background(), t1); err == nil {
		t.Fatalf("expected tenant_required")
	}
}

func TestTenantAggregateGetSuccess(t *testing.T) {
	t.Parallel()
	tnt := newTenantForTest()
	pool := &fakePool{row: fakeTenantRow(tnt)}
	repo := &TenantAggregateRepository{pool: pool}
	got, err := repo.Get(context.Background(), tnt.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Slug != tnt.Slug {
		t.Fatalf("slug = %s", got.Slug)
	}
}

func TestTenantAggregateGetNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &TenantAggregateRepository{pool: pool}
	if _, err := repo.Get(context.Background(), "ghost"); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTenantAggregateGetBySlugSuccess(t *testing.T) {
	t.Parallel()
	tnt := newTenantForTest()
	pool := &fakePool{row: fakeTenantRow(tnt)}
	repo := &TenantAggregateRepository{pool: pool}
	got, err := repo.GetBySlug(context.Background(), "acme")
	if err != nil {
		t.Fatalf("GetBySlug: %v", err)
	}
	if got.ID != tnt.ID {
		t.Fatalf("id = %s", got.ID)
	}
}

func TestTenantAggregateGetBySlugNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{row: fakeRow{err: pgx.ErrNoRows}}
	repo := &TenantAggregateRepository{pool: pool}
	if _, err := repo.GetBySlug(context.Background(), "ghost"); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTenantAggregateList(t *testing.T) {
	t.Parallel()
	tnt := newTenantForTest()
	pool := &fakePool{
		row:  fakeRow{values: []any{1}},
		rows: &fakeRows{rows: [][]any{fakeTenantRow(tnt).values}},
	}
	repo := &TenantAggregateRepository{pool: pool}
	got, total, err := repo.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(got) != 1 {
		t.Fatalf("got total=%d len=%d", total, len(got))
	}
}

func TestTenantAggregateSaveStatusSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 1")}
	repo := &TenantAggregateRepository{pool: pool}
	tnt := newTenantForTest()
	tnt.Status = tenant.StatusActive
	if err := repo.SaveStatus(context.Background(), tnt); err != nil {
		t.Fatalf("SaveStatus: %v", err)
	}
}

func TestTenantAggregateSaveStatusNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &TenantAggregateRepository{pool: pool}
	if err := repo.SaveStatus(context.Background(), newTenantForTest()); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestTenantAggregateUpdateSuccess(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 1")}
	repo := &TenantAggregateRepository{pool: pool}
	if err := repo.Update(context.Background(), newTenantForTest()); err != nil {
		t.Fatalf("Update: %v", err)
	}
}

func TestTenantAggregateUpdateNotFound(t *testing.T) {
	t.Parallel()
	pool := &fakePool{commandTag: pgconn.NewCommandTag("UPDATE 0")}
	repo := &TenantAggregateRepository{pool: pool}
	if err := repo.Update(context.Background(), newTenantForTest()); !errors.Is(err, tenant.ErrTenantNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewTenantAggregateRepository(t *testing.T) {
	t.Parallel()
	if NewTenantAggregateRepository(nil) == nil {
		t.Fatalf("constructor returned nil")
	}
}
