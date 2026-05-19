// File scope: v6.1.0 CF-15 unit tests for the Postgres-backed
// webhook IdempotencyStore adapter.
//
// Real-Postgres coverage lives behind the `integration_pg` build
// tag in webhook_idempotency_integration_test.go; this file
// exercises the adapter through a fake productStore so the default
// `go test ./...` lane keeps its hermetic profile.
package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/nfsarch33/helixon-ec/internal/webhook"
)

// fakeIdempotencyRow drives the QueryRow Scan path for the
// WebhookIdempotencyStore adapter.
type fakeIdempotencyRow struct {
	tenantID string
	err      error
}

func (r *fakeIdempotencyRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 1 {
		return errors.New("expected 1 dest col")
	}
	*(dest[0].(*string)) = r.tenantID
	return nil
}

// fakeIdempotencyPool is a productStore stub that maps a composite
// (tenantID + key) to a row outcome.
type fakeIdempotencyPool struct {
	rows map[string]*fakeIdempotencyRow
}

func (p *fakeIdempotencyPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not used")
}

func (p *fakeIdempotencyPool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("not used")
}

func (p *fakeIdempotencyPool) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if len(args) != 2 {
		return &fakeIdempotencyRow{err: errors.New("expected 2 args")}
	}
	tenantID, _ := args[0].(string)
	key, _ := args[1].(string)
	composite := tenantID + "\x00" + key
	if r, ok := p.rows[composite]; ok {
		return r
	}
	return &fakeIdempotencyRow{err: pgx.ErrNoRows}
}

// TestWebhookIdempotencyStore_FirstObservationReturnsTrue pins the
// happy path: a new (tenant, key) returns true.
func TestWebhookIdempotencyStore_FirstObservationReturnsTrue(t *testing.T) {
	t.Parallel()
	pool := &fakeIdempotencyPool{rows: map[string]*fakeIdempotencyRow{
		"tenantA\x00key1": {tenantID: "tenantA"},
	}}
	store := &WebhookIdempotencyStore{pool: pool}
	ok, err := store.Reserve(context.Background(), "tenantA", "key1")
	if err != nil {
		t.Fatalf("Reserve: %v", err)
	}
	if !ok {
		t.Fatal("Reserve first observation: got false, want true")
	}
}

// TestWebhookIdempotencyStore_DuplicateReturnsFalse pins the
// duplicate path: ON CONFLICT DO NOTHING surfaces as pgx.ErrNoRows
// from RETURNING, which the adapter translates to (false, nil).
func TestWebhookIdempotencyStore_DuplicateReturnsFalse(t *testing.T) {
	t.Parallel()
	pool := &fakeIdempotencyPool{rows: map[string]*fakeIdempotencyRow{
		"tenantA\x00duped": {err: pgx.ErrNoRows},
	}}
	store := &WebhookIdempotencyStore{pool: pool}
	ok, err := store.Reserve(context.Background(), "tenantA", "duped")
	if err != nil {
		t.Fatalf("Reserve duplicate: err=%v, want nil", err)
	}
	if ok {
		t.Fatal("Reserve duplicate: got true, want false")
	}
}

// TestWebhookIdempotencyStore_EmptyInputsSurfaceWebhookSentinel
// pins the input-validation shape so the adapter matches the
// in-memory implementation.
func TestWebhookIdempotencyStore_EmptyInputsSurfaceWebhookSentinel(t *testing.T) {
	t.Parallel()
	store := &WebhookIdempotencyStore{pool: &fakeIdempotencyPool{}}
	for _, tc := range []struct {
		name     string
		tenantID string
		key      string
	}{
		{"empty tenant", "", "key"},
		{"empty key", "tenantA", ""},
		{"both empty", "", ""},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ok, err := store.Reserve(context.Background(), tc.tenantID, tc.key)
			if !errors.Is(err, webhook.ErrWebhookPayloadInvalid) {
				t.Fatalf("Reserve(%q,%q): err=%v, want ErrWebhookPayloadInvalid", tc.tenantID, tc.key, err)
			}
			if ok {
				t.Fatalf("Reserve(%q,%q) returned true on invalid input", tc.tenantID, tc.key)
			}
		})
	}
}

// TestWebhookIdempotencyStore_GenericDatabaseErrorSurfacesWrapped
// verifies non-ErrNoRows database errors are wrapped, not collapsed
// into (false, nil).
func TestWebhookIdempotencyStore_GenericDatabaseErrorSurfacesWrapped(t *testing.T) {
	t.Parallel()
	pool := &fakeIdempotencyPool{rows: map[string]*fakeIdempotencyRow{
		"tenantA\x00boom": {err: errors.New("connection reset")},
	}}
	store := &WebhookIdempotencyStore{pool: pool}
	ok, err := store.Reserve(context.Background(), "tenantA", "boom")
	if err == nil {
		t.Fatal("Reserve db-err: nil err, want wrapped error")
	}
	if errors.Is(err, webhook.ErrWebhookPayloadInvalid) {
		t.Fatalf("Reserve db-err: should NOT alias ErrWebhookPayloadInvalid: %v", err)
	}
	if ok {
		t.Fatal("Reserve db-err: returned true")
	}
}

// TestWebhookIdempotencyStore_ImplementsWebhookPort statically pins
// the adapter to the port surface; a regression there fails the
// build, but this test surfaces it as a coverage line too.
func TestWebhookIdempotencyStore_ImplementsWebhookPort(t *testing.T) {
	t.Parallel()
	var _ webhook.IdempotencyStore = (*WebhookIdempotencyStore)(nil)
}
