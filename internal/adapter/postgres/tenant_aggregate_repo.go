package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/helixon-ec/internal/tenant"
)

// TenantAggregateRepository is the postgres adapter for the tenant
// aggregate. It is a separate type from TenantSettingsRepository
// because the lifecycle row and the settings row have different
// audit characteristics.
type TenantAggregateRepository struct {
	pool productStore
}

// NewTenantAggregateRepository constructs the repo over a pgx pool.
func NewTenantAggregateRepository(pool *pgxpool.Pool) *TenantAggregateRepository {
	return &TenantAggregateRepository{pool: pool}
}

// Create inserts a new tenant row.
func (r *TenantAggregateRepository) Create(ctx context.Context, t tenant.Tenant) error {
	if _, err := tenant.RequireID(t.ID); err != nil {
		return err
	}
	const q = `
		INSERT INTO tenants (id, slug, name, plan, status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err := r.pool.Exec(ctx, q,
		string(t.ID), t.Slug, t.Name, t.Plan, string(t.Status),
		t.CreatedAt, t.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: slug=%s", tenant.ErrTenantSlugAlreadyExists, t.Slug)
		}
		return fmt.Errorf("insert tenant %s: %w", t.ID, err)
	}
	return nil
}

// Get returns the tenant for the given id.
func (r *TenantAggregateRepository) Get(ctx context.Context, id tenant.ID) (tenant.Tenant, error) {
	id, err := tenant.RequireID(id)
	if err != nil {
		return tenant.Tenant{}, err
	}
	const q = `
		SELECT id, slug, name, plan, status, created_at, updated_at
		FROM tenants WHERE id = $1`
	t, err := scanTenantRow(r.pool.QueryRow(ctx, q, string(id)))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tenant.Tenant{}, fmt.Errorf("%w: id=%s", tenant.ErrTenantNotFound, id)
		}
		return tenant.Tenant{}, err
	}
	return t, nil
}

// GetBySlug returns the tenant for the given slug.
func (r *TenantAggregateRepository) GetBySlug(ctx context.Context, slug string) (tenant.Tenant, error) {
	const q = `
		SELECT id, slug, name, plan, status, created_at, updated_at
		FROM tenants WHERE slug = $1`
	t, err := scanTenantRow(r.pool.QueryRow(ctx, q, slug))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return tenant.Tenant{}, fmt.Errorf("%w: slug=%s", tenant.ErrTenantNotFound, slug)
		}
		return tenant.Tenant{}, err
	}
	return t, nil
}

// List returns paginated tenants sorted by created_at DESC.
func (r *TenantAggregateRepository) List(ctx context.Context, page, perPage int) ([]tenant.Tenant, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	const countQ = `SELECT COUNT(*) FROM tenants`
	var total int
	if err := r.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenants: %w", err)
	}
	const q = `
		SELECT id, slug, name, plan, status, created_at, updated_at
		FROM tenants ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenants: %w", err)
	}
	defer rows.Close()
	out := make([]tenant.Tenant, 0, perPage)
	for rows.Next() {
		t, err := scanTenantRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, t)
	}
	return out, total, rows.Err()
}

// SaveStatus persists a status-only update.
func (r *TenantAggregateRepository) SaveStatus(ctx context.Context, t tenant.Tenant) error {
	id, err := tenant.RequireID(t.ID)
	if err != nil {
		return err
	}
	const q = `
		UPDATE tenants SET status = $2, updated_at = $3 WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, string(id), string(t.Status), t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("save status %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: id=%s", tenant.ErrTenantNotFound, id)
	}
	return nil
}

// Update persists a name/plan update.
func (r *TenantAggregateRepository) Update(ctx context.Context, t tenant.Tenant) error {
	id, err := tenant.RequireID(t.ID)
	if err != nil {
		return err
	}
	const q = `
		UPDATE tenants SET name = $2, plan = $3, updated_at = $4 WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q, string(id), t.Name, t.Plan, t.UpdatedAt)
	if err != nil {
		return fmt.Errorf("update tenant %s: %w", id, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: id=%s", tenant.ErrTenantNotFound, id)
	}
	return nil
}

// scanTenantRow reads one row into a tenant.Tenant.
func scanTenantRow(row pgx.Row) (tenant.Tenant, error) {
	var (
		t         tenant.Tenant
		id, slug  string
		name      string
		plan      string
		status    string
		createdAt time.Time
		updatedAt time.Time
	)
	if err := row.Scan(&id, &slug, &name, &plan, &status, &createdAt, &updatedAt); err != nil {
		return tenant.Tenant{}, err
	}
	t.ID = tenant.ID(id)
	t.Slug = slug
	t.Name = name
	t.Plan = plan
	t.Status = tenant.Status(status)
	t.CreatedAt = createdAt.UTC()
	t.UpdatedAt = updatedAt.UTC()
	return t, nil
}
