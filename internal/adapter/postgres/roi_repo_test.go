package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/nfsarch33/helixon-ec/internal/api/handler"
	"github.com/stretchr/testify/require"
)

// fakeRows + fakeROIPool let us drive the ROIRepository without a
// Postgres container. The fake returns one or more pre-baked rows
// that scan into the handler.ROIPoint shape.
type fakeROIRow struct {
	Day                  time.Time
	Channel              string
	ProductID            string
	TotalRevenueAUDCents int64
	GrossProfitAUDCents  int64
	OrderCount           int64
	TotalCostAUDCents    int64
}

type fakeROIRows struct {
	rows []fakeROIRow
	idx  int
	err  error
}

func (r *fakeROIRows) Close()                                       {}
func (r *fakeROIRows) Err() error                                   { return r.err }
func (r *fakeROIRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakeROIRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakeROIRows) RawValues() [][]byte                          { return nil }
func (r *fakeROIRows) Conn() *pgx.Conn                              { return nil }
func (r *fakeROIRows) Values() ([]any, error)                       { return nil, nil }

func (r *fakeROIRows) Next() bool {
	if r.err != nil {
		return false
	}
	if r.idx >= len(r.rows) {
		return false
	}
	r.idx++
	return true
}

func (r *fakeROIRows) Scan(dest ...any) error {
	if len(dest) != 7 {
		return errors.New("expected 7 dest cols")
	}
	row := r.rows[r.idx-1]
	*(dest[0].(*time.Time)) = row.Day
	*(dest[1].(*string)) = row.Channel
	*(dest[2].(*string)) = row.ProductID
	*(dest[3].(*int64)) = row.TotalRevenueAUDCents
	*(dest[4].(*int64)) = row.GrossProfitAUDCents
	*(dest[5].(*int64)) = row.OrderCount
	*(dest[6].(*int64)) = row.TotalCostAUDCents
	return nil
}

type fakeROIPool struct {
	rows []fakeROIRow
	err  error
}

func (p *fakeROIPool) Exec(_ context.Context, _ string, _ ...any) (pgconn.CommandTag, error) {
	return pgconn.CommandTag{}, errors.New("not used")
}

func (p *fakeROIPool) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	if p.err != nil {
		return nil, p.err
	}
	return &fakeROIRows{rows: p.rows}, nil
}

func (p *fakeROIPool) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return nil
}

func TestROIRepository_Heatmap_ScansRowsCorrectly(t *testing.T) {
	t.Parallel()
	pool := &fakeROIPool{rows: []fakeROIRow{
		{Day: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Channel: "tiktok", ProductID: "p1", TotalRevenueAUDCents: 10000, GrossProfitAUDCents: 4000, OrderCount: 5, TotalCostAUDCents: 6000},
		{Day: time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC), Channel: "facebook", ProductID: "p2", TotalRevenueAUDCents: 5000, GrossProfitAUDCents: 1000, OrderCount: 2, TotalCostAUDCents: 4000},
	}}
	repo := &ROIRepository{pool: pool}
	rows, err := repo.Heatmap(context.Background(), handler.ROIFilter{
		TenantID: "tenant-A",
		From:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, rows, 2)
	require.Equal(t, "tiktok", rows[0].Channel)
	require.EqualValues(t, 10000, rows[0].TotalRevenueAUDCents)
}

func TestROIRepository_Heatmap_QueryError(t *testing.T) {
	t.Parallel()
	pool := &fakeROIPool{err: errors.New("conn refused")}
	repo := &ROIRepository{pool: pool}
	_, err := repo.Heatmap(context.Background(), handler.ROIFilter{TenantID: "tenant-A"})
	require.Error(t, err)
}

func TestROIRepository_DeadStock_FiltersByCutoff(t *testing.T) {
	t.Parallel()
	pool := &fakeROIPool{rows: []fakeROIRow{
		{Day: time.Now().AddDate(0, 0, -90), Channel: "tiktok", ProductID: "p-slow", OrderCount: 0, TotalCostAUDCents: 5000},
	}}
	repo := &ROIRepository{pool: pool}
	rows, err := repo.DeadStock(context.Background(), handler.ROIFilter{TenantID: "tenant-A", MinAgeDays: 60})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}

func TestROIRepository_ByChannel_ReturnsRows(t *testing.T) {
	t.Parallel()
	pool := &fakeROIPool{rows: []fakeROIRow{
		{Day: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC), Channel: "tiktok", TotalRevenueAUDCents: 10000, OrderCount: 5, TotalCostAUDCents: 6000},
	}}
	repo := &ROIRepository{pool: pool}
	rows, err := repo.ByChannel(context.Background(), handler.ROIFilter{
		TenantID: "tenant-A",
		From:     time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		To:       time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	})
	require.NoError(t, err)
	require.Len(t, rows, 1)
}
