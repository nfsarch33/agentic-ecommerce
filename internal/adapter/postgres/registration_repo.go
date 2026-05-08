package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/registration"
)

// RegistrationRepository is the postgres adapter for the v2.5.0
// tenant_registrations table.
type RegistrationRepository struct {
	pool productStore
}

// NewRegistrationRepository constructs the repository.
func NewRegistrationRepository(pool *pgxpool.Pool) *RegistrationRepository {
	return &RegistrationRepository{pool: pool}
}

// Create inserts a new registration row.
func (r *RegistrationRepository) Create(ctx context.Context, req registration.Request) error {
	const q = `
		INSERT INTO tenant_registrations (
			id, email, slug_requested, plan_requested, status, tenant_id, company_name,
			created_at, verified_at, onboarded_at, activated_at, expires_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err := r.pool.Exec(ctx, q,
		req.ID, req.Email, req.SlugRequested, req.PlanRequested,
		string(req.Status), req.TenantID, req.CompanyName,
		req.CreatedAt, nullTime(req.VerifiedAt), nullTime(req.OnboardedAt),
		nullTime(req.ActivatedAt), req.ExpiresAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: id=%s", registration.ErrRequestAlreadyExists, req.ID)
		}
		return fmt.Errorf("insert tenant_registration %s: %w", req.ID, err)
	}
	return nil
}

// Get returns the row with id.
func (r *RegistrationRepository) Get(ctx context.Context, id string) (registration.Request, error) {
	const q = `
		SELECT id, email, slug_requested, plan_requested, status, tenant_id, company_name,
		       created_at, verified_at, onboarded_at, activated_at, expires_at
		FROM tenant_registrations WHERE id = $1`
	row := r.pool.QueryRow(ctx, q, id)
	req, err := scanRegistration(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return registration.Request{}, fmt.Errorf("%w: id=%s", registration.ErrRequestNotFound, id)
		}
		return registration.Request{}, err
	}
	return req, nil
}

// GetByEmail returns the most recent active|pending|verified row for
// email, falling back to the most recent overall.
func (r *RegistrationRepository) GetByEmail(ctx context.Context, email string) (registration.Request, error) {
	const q = `
		SELECT id, email, slug_requested, plan_requested, status, tenant_id, company_name,
		       created_at, verified_at, onboarded_at, activated_at, expires_at
		FROM tenant_registrations
		WHERE email = $1
		ORDER BY created_at DESC
		LIMIT 1`
	row := r.pool.QueryRow(ctx, q, email)
	req, err := scanRegistration(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return registration.Request{}, fmt.Errorf("%w: email=%s", registration.ErrRequestNotFound, email)
		}
		return registration.Request{}, err
	}
	return req, nil
}

// List returns paginated rows sorted by created_at desc.
func (r *RegistrationRepository) List(ctx context.Context, page, perPage int) ([]registration.Request, int, error) {
	page, perPage = normalize(page, perPage)
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM tenant_registrations`).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count tenant_registrations: %w", err)
	}
	const q = `
		SELECT id, email, slug_requested, plan_requested, status, tenant_id, company_name,
		       created_at, verified_at, onboarded_at, activated_at, expires_at
		FROM tenant_registrations
		ORDER BY created_at DESC
		LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("list tenant_registrations: %w", err)
	}
	defer rows.Close()
	out := make([]registration.Request, 0, perPage)
	for rows.Next() {
		req, err := scanRegistration(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, req)
	}
	return out, total, rows.Err()
}

// Save persists a status update.
func (r *RegistrationRepository) Save(ctx context.Context, req registration.Request) error {
	const q = `
		UPDATE tenant_registrations
		SET email = $2,
		    slug_requested = $3,
		    plan_requested = $4,
		    status = $5,
		    tenant_id = $6,
		    company_name = $7,
		    verified_at = $8,
		    onboarded_at = $9,
		    activated_at = $10,
		    expires_at = $11
		WHERE id = $1`
	tag, err := r.pool.Exec(ctx, q,
		req.ID, req.Email, req.SlugRequested, req.PlanRequested,
		string(req.Status), req.TenantID, req.CompanyName,
		nullTime(req.VerifiedAt), nullTime(req.OnboardedAt),
		nullTime(req.ActivatedAt), req.ExpiresAt,
	)
	if err != nil {
		return fmt.Errorf("save tenant_registration %s: %w", req.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: id=%s", registration.ErrRequestNotFound, req.ID)
	}
	return nil
}

func scanRegistration(row pgx.Row) (registration.Request, error) {
	var (
		req      registration.Request
		status   string
		verified *time.Time
		onboard  *time.Time
		active   *time.Time
	)
	if err := row.Scan(
		&req.ID, &req.Email, &req.SlugRequested, &req.PlanRequested,
		&status, &req.TenantID, &req.CompanyName,
		&req.CreatedAt, &verified, &onboard, &active, &req.ExpiresAt,
	); err != nil {
		return registration.Request{}, err
	}
	req.Status = registration.Status(status)
	if verified != nil {
		req.VerifiedAt = verified.UTC()
	}
	if onboard != nil {
		req.OnboardedAt = onboard.UTC()
	}
	if active != nil {
		req.ActivatedAt = active.UTC()
	}
	return req, nil
}
