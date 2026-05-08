package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/jackc/pgx/v5"
)

// TenantConnAcquirer is the minimum surface a TenantPool needs from
// pgxpool.Pool. Tests inject a fake; production uses *pgxpool.Pool.
type TenantConnAcquirer interface {
	Acquire(ctx context.Context) (TenantConn, error)
}

// TenantConn is the per-connection surface used while a tenant GUC
// is set. A *pgxpool.Conn satisfies this surface via the
// pgxConnAdapter wrapper.
type TenantConn interface {
	Exec(ctx context.Context, sql string, args ...any) (any, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Release()
}

// tenantPattern enforces the same shape used by RLS GUC writes.
// Permitting only safe characters means we never have to escape.
var tenantPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// ErrInvalidTenantID is returned when WithTenant is called with a
// tenantID that does not match tenantPattern.
var ErrInvalidTenantID = errors.New("postgres: invalid tenant id")

// WithTenant runs fn against a connection that has the
// app.current_tenant_id GUC set to tenantID for the connection's
// lifetime. It mirrors the pattern from migrations/0011_rls.up.sql:
// the GUC drives every RLS policy, so callers running outside an
// admin context MUST acquire connections via WithTenant rather than
// from the bare pool to keep RLS enforced.
//
// The function is intentionally tiny so the postgres adapter
// retains its existing query-by-pool style for admin contexts and
// can opt in to tenant scoping per call site without a registry-
// wide refactor.
func WithTenant(ctx context.Context, pool TenantConnAcquirer, tenantID string, fn func(TenantConn) error) error {
	if tenantID == "" || !tenantPattern.MatchString(tenantID) {
		return fmt.Errorf("%w: tenantID=%q", ErrInvalidTenantID, tenantID)
	}
	if pool == nil {
		return errors.New("postgres: pool is nil")
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return fmt.Errorf("acquire tenant conn: %w", err)
	}
	defer conn.Release()
	if _, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', $1, false)", tenantID); err != nil {
		return fmt.Errorf("set tenant guc: %w", err)
	}
	return fn(conn)
}

// ResetTenant clears the GUC for the connection. Callers that
// stash a connection longer than a single WithTenant invocation can
// use this to drop back to admin context.
func ResetTenant(ctx context.Context, conn TenantConn) error {
	if conn == nil {
		return errors.New("postgres: conn is nil")
	}
	if _, err := conn.Exec(ctx, "SELECT set_config('app.current_tenant_id', '', false)"); err != nil {
		return fmt.Errorf("reset tenant guc: %w", err)
	}
	return nil
}
