package postgres

import (
	"context"
	"testing"
)

func TestNewRefreshExecutor_NilPoolErrors(t *testing.T) {
	t.Parallel()
	if _, err := NewRefreshExecutor(nil); err == nil {
		t.Fatal("expected error for nil pool")
	}
}

func TestRefreshExecutor_NotInitialised_Errors(t *testing.T) {
	t.Parallel()
	var r *RefreshExecutor
	if _, err := r.ExecRefresh(context.Background(), "REFRESH MATERIALIZED VIEW gmv_daily_rollup"); err == nil {
		t.Fatal("expected error for nil receiver")
	}
	r2 := &RefreshExecutor{}
	if _, err := r2.ExecRefresh(context.Background(), "REFRESH MATERIALIZED VIEW gmv_daily_rollup"); err == nil {
		t.Fatal("expected error for empty pool")
	}
}
