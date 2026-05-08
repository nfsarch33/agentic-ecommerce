package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

// File scope: smoke-level construction tests for every public repository
// constructor in this package. The constructors are trivial (struct
// initialisation), but they previously sat at 0% coverage because they
// expected a real *pgxpool.Pool. Constructing with a parsed-but-not-dialed
// pool exercises the real signatures with no Docker dependency.

func newDummyPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	cfg, err := pgxpool.ParseConfig("postgres://does-not:resolve@127.0.0.1:1/agentic?sslmode=disable")
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	cfg.ConnConfig.ConnectTimeout = 100 * time.Millisecond
	pool, err := pgxpool.NewWithConfig(context.Background(), cfg)
	if err != nil {
		t.Fatalf("NewWithConfig: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func TestNewProductRepositoryReturnsConfiguredRepo(t *testing.T) {
	t.Parallel()

	pool := newDummyPool(t)
	repo := NewProductRepository(pool)
	if repo == nil {
		t.Fatal("NewProductRepository returned nil")
	}
	if repo.pool == nil {
		t.Fatal("repo.pool not initialised")
	}
}

func TestNewOrderRepositoryReturnsConfiguredRepo(t *testing.T) {
	t.Parallel()

	pool := newDummyPool(t)
	repo := NewOrderRepository(pool)
	if repo == nil || repo.pool == nil {
		t.Fatalf("NewOrderRepository returned %#v", repo)
	}
}

func TestNewCartRepositoryReturnsConfiguredRepo(t *testing.T) {
	t.Parallel()

	pool := newDummyPool(t)
	repo := NewCartRepository(pool)
	if repo == nil || repo.pool == nil {
		t.Fatalf("NewCartRepository returned %#v", repo)
	}
}

func TestNewTenantSettingsRepositoryReturnsConfiguredRepo(t *testing.T) {
	t.Parallel()

	pool := newDummyPool(t)
	repo := NewTenantSettingsRepository(pool)
	if repo == nil || repo.pool == nil {
		t.Fatalf("NewTenantSettingsRepository returned %#v", repo)
	}
}

func TestTenantSettingsRepositoryPutSettingsRequiresTenant(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{}
	repo := &TenantSettingsRepository{pool: store}

	err := repo.PutSettings(context.Background(), tenant.Settings{})
	if !errors.Is(err, tenant.ErrTenantRequired) {
		t.Fatalf("PutSettings err = %v, want ErrTenantRequired", err)
	}
	if len(store.execCalls) != 0 {
		t.Fatalf("exec calls = %d, want 0 for missing tenant", len(store.execCalls))
	}
}

func TestTenantSettingsRepositoryGetSettingsRequiresTenant(t *testing.T) {
	t.Parallel()

	store := &mockProductStore{}
	repo := &TenantSettingsRepository{pool: store}

	if _, err := repo.GetSettings(context.Background(), tenant.ID("   ")); !errors.Is(err, tenant.ErrTenantRequired) {
		t.Fatalf("GetSettings err = %v, want ErrTenantRequired", err)
	}
	if len(store.queryRowCalls) != 0 {
		t.Fatalf("queryRow calls = %d, want 0 for missing tenant", len(store.queryRowCalls))
	}
}

func TestTenantSettingsRepositoryGetSettingsWrapsScanError(t *testing.T) {
	t.Parallel()

	want := errors.New("scan boom")
	store := &mockProductStore{
		rowResult: &mockRow{scanFn: func(_ ...any) error { return want }},
	}
	repo := &TenantSettingsRepository{pool: store}

	_, err := repo.GetSettings(context.Background(), "tenant-a")
	if !errors.Is(err, want) {
		t.Fatalf("GetSettings err = %v, want wrapped %v", err, want)
	}
}

func TestDecodeTenantJSONIsNoOpForEmptyInput(t *testing.T) {
	t.Parallel()

	var out map[string]any
	if err := decodeTenantJSON(nil, &out); err != nil {
		t.Fatalf("nil bytes: %v", err)
	}
	if err := decodeTenantJSON([]byte{}, &out); err != nil {
		t.Fatalf("empty bytes: %v", err)
	}
	if out != nil {
		t.Fatalf("decodeTenantJSON populated empty input: %v", out)
	}
}

func TestDecodeTenantJSONReturnsParseErrorForInvalidPayload(t *testing.T) {
	t.Parallel()

	var out map[string]any
	if err := decodeTenantJSON([]byte("not-json"), &out); err == nil {
		t.Fatal("decodeTenantJSON accepted invalid JSON")
	}
}
