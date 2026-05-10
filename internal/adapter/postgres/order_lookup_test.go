package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"
)

// fakeOrderLookupRow drives the QueryRow Scan path for the
// OrderLookupRepository test without spinning up a real Postgres.
type fakeOrderLookupRow struct {
	orderID  string
	tenantID string
	err      error
}

func (r *fakeOrderLookupRow) Scan(dest ...any) error {
	if r.err != nil {
		return r.err
	}
	if len(dest) != 2 {
		return errors.New("expected 2 dest cols")
	}
	*(dest[0].(*string)) = r.orderID
	*(dest[1].(*string)) = r.tenantID
	return nil
}

// fakeOrderLookupPool is a tiny productStore stub so the test can
// run without a Postgres container.
type fakeOrderLookupPool struct {
	rows map[string]*fakeOrderLookupRow
}

func (p *fakeOrderLookupPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not used")
}

func (p *fakeOrderLookupPool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, errors.New("not used")
}

func (p *fakeOrderLookupPool) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	if len(args) == 0 {
		return &fakeOrderLookupRow{err: errors.New("no args")}
	}
	tn, _ := args[0].(string)
	if r, ok := p.rows[tn]; ok {
		return r
	}
	return &fakeOrderLookupRow{err: pgx.ErrNoRows}
}

func TestOrderLookupRepository_OrderForTracking_Success(t *testing.T) {
	t.Parallel()
	pool := &fakeOrderLookupPool{rows: map[string]*fakeOrderLookupRow{
		"AP-001": {orderID: "ord-001", tenantID: "tenant-A"},
	}}
	repo := &OrderLookupRepository{pool: pool}
	orderID, tenantID, err := repo.OrderForTracking(context.Background(), "AP-001")
	require.NoError(t, err)
	require.Equal(t, "ord-001", orderID)
	require.Equal(t, "tenant-A", tenantID)
}

func TestOrderLookupRepository_OrderForTracking_NotFound(t *testing.T) {
	t.Parallel()
	pool := &fakeOrderLookupPool{rows: map[string]*fakeOrderLookupRow{}}
	repo := &OrderLookupRepository{pool: pool}
	_, _, err := repo.OrderForTracking(context.Background(), "AP-MISSING")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrOrderLookupNotFound))
}

func TestOrderLookupRepository_OrderForTracking_EmptyTracking(t *testing.T) {
	t.Parallel()
	repo := &OrderLookupRepository{pool: &fakeOrderLookupPool{}}
	_, _, err := repo.OrderForTracking(context.Background(), "")
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrOrderLookupNotFound))
}

func TestOrderLookupRepository_OrderForTracking_DatabaseError(t *testing.T) {
	t.Parallel()
	pool := &fakeOrderLookupPool{rows: map[string]*fakeOrderLookupRow{
		"AP-DBERR": {err: errors.New("connection refused")},
	}}
	repo := &OrderLookupRepository{pool: pool}
	_, _, err := repo.OrderForTracking(context.Background(), "AP-DBERR")
	require.Error(t, err)
	require.False(t, errors.Is(err, ErrOrderLookupNotFound), "non-ErrNoRows must NOT surface as ErrOrderLookupNotFound")
}
