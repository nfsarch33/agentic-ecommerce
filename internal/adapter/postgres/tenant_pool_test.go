package postgres_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/nfsarch33/helixon-ec/internal/adapter/postgres"
)

type fakeConn struct {
	execCalls    []execRecord
	execErr      error
	released     bool
	queryErr     error
	queryRowResp pgx.Row
}

type execRecord struct {
	sql  string
	args []any
}

func (f *fakeConn) Exec(_ context.Context, sql string, args ...any) (any, error) {
	f.execCalls = append(f.execCalls, execRecord{sql: sql, args: args})
	return nil, f.execErr
}

func (f *fakeConn) Query(_ context.Context, _ string, _ ...any) (pgx.Rows, error) {
	return nil, f.queryErr
}

func (f *fakeConn) QueryRow(_ context.Context, _ string, _ ...any) pgx.Row {
	return f.queryRowResp
}

func (f *fakeConn) Release() { f.released = true }

type fakePool struct {
	conn       *fakeConn
	acquireErr error
}

func (f *fakePool) Acquire(_ context.Context) (postgres.TenantConn, error) {
	if f.acquireErr != nil {
		return nil, f.acquireErr
	}
	return f.conn, nil
}

func TestWithTenant_SetsGucAndReleases(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{}
	pool := &fakePool{conn: conn}

	called := false
	err := postgres.WithTenant(context.Background(), pool, "tenant-a", func(c postgres.TenantConn) error {
		called = true
		if c != conn {
			t.Fatalf("fn received different conn")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !called {
		t.Fatalf("callback not invoked")
	}
	if !conn.released {
		t.Fatalf("connection not released")
	}
	if len(conn.execCalls) != 1 {
		t.Fatalf("expected 1 SET GUC, got %d", len(conn.execCalls))
	}
	got := conn.execCalls[0]
	if got.sql != "SELECT set_config('app.current_tenant_id', $1, false)" {
		t.Fatalf("guc sql = %q", got.sql)
	}
	if len(got.args) != 1 || got.args[0] != "tenant-a" {
		t.Fatalf("guc args = %v", got.args)
	}
}

func TestWithTenant_RejectsInvalidTenant(t *testing.T) {
	t.Parallel()
	cases := []string{"", " ", "Tenant", "tenant!", "-leading"}
	for _, tc := range cases {
		tc := tc
		t.Run(tc, func(t *testing.T) {
			t.Parallel()
			err := postgres.WithTenant(context.Background(), &fakePool{conn: &fakeConn{}}, tc, func(postgres.TenantConn) error { return nil })
			if !errors.Is(err, postgres.ErrInvalidTenantID) {
				t.Fatalf("err=%v want ErrInvalidTenantID", err)
			}
		})
	}
}

func TestWithTenant_NilPool(t *testing.T) {
	t.Parallel()
	err := postgres.WithTenant(context.Background(), nil, "tenant-a", func(postgres.TenantConn) error { return nil })
	if err == nil {
		t.Fatalf("expected error for nil pool")
	}
}

func TestWithTenant_AcquireErr(t *testing.T) {
	t.Parallel()
	want := errors.New("acquire boom")
	err := postgres.WithTenant(context.Background(), &fakePool{acquireErr: want}, "tenant-a", func(postgres.TenantConn) error { return nil })
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want wrapped %v", err, want)
	}
}

func TestWithTenant_GucSetErr(t *testing.T) {
	t.Parallel()
	want := errors.New("set guc boom")
	conn := &fakeConn{execErr: want}
	pool := &fakePool{conn: conn}
	err := postgres.WithTenant(context.Background(), pool, "tenant-a", func(postgres.TenantConn) error { return nil })
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want wrapped %v", err, want)
	}
	if !conn.released {
		t.Fatalf("connection should still release on guc failure")
	}
}

func TestWithTenant_FnErrPropagates(t *testing.T) {
	t.Parallel()
	want := errors.New("fn boom")
	pool := &fakePool{conn: &fakeConn{}}
	err := postgres.WithTenant(context.Background(), pool, "tenant-a", func(postgres.TenantConn) error { return want })
	if !errors.Is(err, want) {
		t.Fatalf("err=%v want %v", err, want)
	}
}

func TestResetTenant(t *testing.T) {
	t.Parallel()
	conn := &fakeConn{}
	if err := postgres.ResetTenant(context.Background(), conn); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(conn.execCalls) != 1 {
		t.Fatalf("expected 1 reset call, got %d", len(conn.execCalls))
	}
	if conn.execCalls[0].sql != "SELECT set_config('app.current_tenant_id', '', false)" {
		t.Fatalf("reset sql = %q", conn.execCalls[0].sql)
	}
}

func TestResetTenant_NilConn(t *testing.T) {
	t.Parallel()
	if err := postgres.ResetTenant(context.Background(), nil); err == nil {
		t.Fatalf("expected error for nil conn")
	}
}

func TestResetTenant_ExecErr(t *testing.T) {
	t.Parallel()
	want := errors.New("reset boom")
	conn := &fakeConn{execErr: want}
	if err := postgres.ResetTenant(context.Background(), conn); !errors.Is(err, want) {
		t.Fatalf("err=%v want wrapped %v", err, want)
	}
}
