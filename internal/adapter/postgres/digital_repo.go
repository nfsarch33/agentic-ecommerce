package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// DigitalProductRepository is the postgres adapter for the digital
// product context. SQL is intentionally explicit so query plans stay
// reviewable and tenant scoping is the first WHERE clause every time.
type DigitalProductRepository struct {
	pool productStore
}

// NewDigitalProductRepository constructs a DigitalProductRepository
// over a pgx pool.
func NewDigitalProductRepository(pool *pgxpool.Pool) *DigitalProductRepository {
	return &DigitalProductRepository{pool: pool}
}

// Create inserts a new DigitalProduct.
func (r *DigitalProductRepository) Create(ctx context.Context, tenantID string, p digital.DigitalProduct) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if p.TenantID() != tenantID {
		return digital.ErrTenantMismatch
	}
	const q = `
		INSERT INTO digital_products (
			id, tenant_id, sku, name, description, file_path, file_size,
			content_type, checksum, version, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err = r.pool.Exec(ctx, q,
		p.ID(), tenantID, p.SKU(), p.Name(), p.Description(),
		p.FilePath(), p.FileSize(), p.ContentType(), p.Checksum(),
		p.Version(), p.CreatedAt(), p.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert digital product %s (tenant %s): %w", p.ID(), tenantID, err)
	}
	return nil
}

// Update overwrites mutable digital product fields.
func (r *DigitalProductRepository) Update(ctx context.Context, tenantID string, p digital.DigitalProduct) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if p.TenantID() != tenantID {
		return digital.ErrTenantMismatch
	}
	const q = `
		UPDATE digital_products
		SET name = $3, description = $4, file_path = $5, file_size = $6,
		    content_type = $7, checksum = $8, version = $9, updated_at = $10
		WHERE tenant_id = $1 AND id = $2`
	tag, err := r.pool.Exec(ctx, q,
		tenantID, p.ID(), p.Name(), p.Description(), p.FilePath(),
		p.FileSize(), p.ContentType(), p.Checksum(), p.Version(),
		p.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("update digital product %s (tenant %s): %w", p.ID(), tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrDigitalProductNotFound
	}
	return nil
}

// Get fetches a single DigitalProduct.
func (r *DigitalProductRepository) Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.DigitalProduct, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return digital.DigitalProduct{}, err
	}
	const q = `
		SELECT id, tenant_id, sku, name, description, file_path, file_size,
		       content_type, checksum, version, created_at, updated_at
		FROM digital_products
		WHERE tenant_id = $1 AND id = $2`
	rec, err := scanDigitalProductRow(r.pool.QueryRow(ctx, q, tenantID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digital.DigitalProduct{}, port.ErrDigitalProductNotFound
		}
		return digital.DigitalProduct{}, err
	}
	return digital.ReconstructDigitalProduct(rec), nil
}

// List returns DigitalProducts ordered by created_at.
func (r *DigitalProductRepository) List(ctx context.Context, tenantID string, page, perPage int) (port.DigitalProductList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.DigitalProductList{}, err
	}
	page, perPage = normalisePagination(page, perPage)
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, sku, name, description, file_path, file_size,
		       content_type, checksum, version, created_at, updated_at
		FROM digital_products
		WHERE tenant_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3`, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return port.DigitalProductList{}, fmt.Errorf("list digital products (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()
	products := make([]digital.DigitalProduct, 0)
	for rows.Next() {
		rec, scanErr := scanDigitalProductRow(rows)
		if scanErr != nil {
			return port.DigitalProductList{}, scanErr
		}
		products = append(products, digital.ReconstructDigitalProduct(rec))
	}
	if rows.Err() != nil {
		return port.DigitalProductList{}, rows.Err()
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM digital_products WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return port.DigitalProductList{}, fmt.Errorf("count digital products: %w", err)
	}
	return port.DigitalProductList{Products: products, Total: total}, nil
}

// Delete removes a DigitalProduct.
func (r *DigitalProductRepository) Delete(ctx context.Context, tenantID string, id uuid.UUID) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM digital_products WHERE tenant_id = $1 AND id = $2`, tenantID, id)
	if err != nil {
		return fmt.Errorf("delete digital product (tenant %s): %w", tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrDigitalProductNotFound
	}
	return nil
}

func scanDigitalProductRow(row pgx.Row) (digital.DigitalProductRecord, error) {
	var rec digital.DigitalProductRecord
	if err := row.Scan(
		&rec.ID, &rec.TenantID, &rec.SKU, &rec.Name, &rec.Description,
		&rec.FilePath, &rec.FileSize, &rec.ContentType, &rec.Checksum,
		&rec.Version, &rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return digital.DigitalProductRecord{}, err
	}
	return rec, nil
}

// LicenseRepository is the postgres adapter for the licence context.
type LicenseRepository struct {
	pool productStore
}

// NewLicenseRepository constructs a LicenseRepository over a pgx pool.
func NewLicenseRepository(pool *pgxpool.Pool) *LicenseRepository {
	return &LicenseRepository{pool: pool}
}

func (r *LicenseRepository) Create(ctx context.Context, tenantID string, lic digital.License) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if lic.TenantID() != tenantID {
		return digital.ErrTenantMismatch
	}
	const q = `
		INSERT INTO digital_licenses (
			id, tenant_id, product_id, customer_id, key, state, issued_at,
			expires_at, max_activations, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`
	var expires any
	if !lic.ExpiresAt().IsZero() {
		expires = lic.ExpiresAt()
	}
	_, err = r.pool.Exec(ctx, q,
		lic.ID(), tenantID, lic.ProductID(), lic.CustomerID(), lic.Key(),
		string(lic.State()), lic.IssuedAt(), expires, lic.MaxActivations(),
		lic.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert licence %s (tenant %s): %w", lic.ID(), tenantID, err)
	}
	return nil
}

func (r *LicenseRepository) Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.License, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return digital.License{}, err
	}
	const q = `
		SELECT id, tenant_id, product_id, customer_id, key, state, issued_at,
		       expires_at, max_activations, updated_at
		FROM digital_licenses
		WHERE tenant_id = $1 AND id = $2`
	rec, err := scanLicenseRow(r.pool.QueryRow(ctx, q, tenantID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digital.License{}, port.ErrLicenseNotFound
		}
		return digital.License{}, err
	}
	return digital.ReconstructLicense(rec), nil
}

func (r *LicenseRepository) List(ctx context.Context, tenantID string, page, perPage int) (port.LicenseList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.LicenseList{}, err
	}
	page, perPage = normalisePagination(page, perPage)
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, product_id, customer_id, key, state, issued_at,
		       expires_at, max_activations, updated_at
		FROM digital_licenses
		WHERE tenant_id = $1
		ORDER BY issued_at ASC
		LIMIT $2 OFFSET $3`, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return port.LicenseList{}, fmt.Errorf("list licences (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()
	return collectLicenseRows(ctx, r.pool, tenantID, rows, "")
}

func (r *LicenseRepository) ListByCustomer(ctx context.Context, tenantID string, customerID uuid.UUID, page, perPage int) (port.LicenseList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.LicenseList{}, err
	}
	page, perPage = normalisePagination(page, perPage)
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, product_id, customer_id, key, state, issued_at,
		       expires_at, max_activations, updated_at
		FROM digital_licenses
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY issued_at ASC
		LIMIT $3 OFFSET $4`, tenantID, customerID, perPage, (page-1)*perPage)
	if err != nil {
		return port.LicenseList{}, fmt.Errorf("list licences for customer (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()
	return collectLicenseRows(ctx, r.pool, tenantID, rows, customerID.String())
}

func (r *LicenseRepository) SaveState(ctx context.Context, tenantID string, lic digital.License) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if lic.TenantID() != tenantID {
		return digital.ErrTenantMismatch
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE digital_licenses
		SET state = $3, updated_at = $4
		WHERE tenant_id = $1 AND id = $2`,
		tenantID, lic.ID(), string(lic.State()), lic.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("save licence state %s (tenant %s): %w", lic.ID(), tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrLicenseNotFound
	}
	return nil
}

func collectLicenseRows(ctx context.Context, pool productStore, tenantID string, rows pgx.Rows, customerID string) (port.LicenseList, error) {
	licenses := make([]digital.License, 0)
	for rows.Next() {
		rec, scanErr := scanLicenseRow(rows)
		if scanErr != nil {
			return port.LicenseList{}, scanErr
		}
		licenses = append(licenses, digital.ReconstructLicense(rec))
	}
	if rows.Err() != nil {
		return port.LicenseList{}, rows.Err()
	}
	var total int
	if customerID == "" {
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM digital_licenses WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
			return port.LicenseList{}, fmt.Errorf("count licences: %w", err)
		}
	} else {
		if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM digital_licenses WHERE tenant_id = $1 AND customer_id = $2`, tenantID, customerID).Scan(&total); err != nil {
			return port.LicenseList{}, fmt.Errorf("count licences for customer: %w", err)
		}
	}
	return port.LicenseList{Licenses: licenses, Total: total}, nil
}

func scanLicenseRow(row pgx.Row) (digital.LicenseRecord, error) {
	var rec digital.LicenseRecord
	var state string
	var expires *time.Time
	if err := row.Scan(
		&rec.ID, &rec.TenantID, &rec.ProductID, &rec.CustomerID, &rec.Key,
		&state, &rec.IssuedAt, &expires, &rec.MaxActivations, &rec.UpdatedAt,
	); err != nil {
		return digital.LicenseRecord{}, err
	}
	parsed, err := digital.ParseState(state)
	if err != nil {
		return digital.LicenseRecord{}, fmt.Errorf("scan licence state %q: %w", state, err)
	}
	rec.State = parsed
	if expires != nil {
		rec.ExpiresAt = *expires
	}
	return rec, nil
}

// AccessGrantRepository is the postgres adapter for the access-grant
// context. Upserts hit the (tenant_id, customer_id, product_id) unique
// constraint so re-purchases stay idempotent.
type AccessGrantRepository struct {
	pool productStore
}

// NewAccessGrantRepository constructs an AccessGrantRepository.
func NewAccessGrantRepository(pool *pgxpool.Pool) *AccessGrantRepository {
	return &AccessGrantRepository{pool: pool}
}

func (r *AccessGrantRepository) Upsert(ctx context.Context, tenantID string, grant digital.AccessGrant) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if grant.TenantID() != tenantID {
		return digital.ErrTenantMismatch
	}
	const q = `
		INSERT INTO digital_access_grants (
			id, tenant_id, customer_id, product_id, license_id, granted_at, source
		) VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (tenant_id, customer_id, product_id)
		DO UPDATE SET license_id = EXCLUDED.license_id,
		              granted_at = EXCLUDED.granted_at,
		              source = EXCLUDED.source`
	if _, err := r.pool.Exec(ctx, q,
		grant.ID(), tenantID, grant.CustomerID(), grant.ProductID(),
		grant.LicenseID(), grant.GrantedAt(), string(grant.Source()),
	); err != nil {
		return fmt.Errorf("upsert access grant %s (tenant %s): %w", grant.ID(), tenantID, err)
	}
	return nil
}

func (r *AccessGrantRepository) Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.AccessGrant, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return digital.AccessGrant{}, err
	}
	rec, err := scanAccessGrantRow(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, customer_id, product_id, license_id, granted_at, source
		FROM digital_access_grants
		WHERE tenant_id = $1 AND id = $2`, tenantID, id))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digital.AccessGrant{}, port.ErrAccessGrantNotFound
		}
		return digital.AccessGrant{}, err
	}
	return digital.ReconstructAccessGrant(rec), nil
}

func (r *AccessGrantRepository) ListByCustomer(ctx context.Context, tenantID string, customerID uuid.UUID, page, perPage int) (port.AccessGrantList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.AccessGrantList{}, err
	}
	page, perPage = normalisePagination(page, perPage)
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, customer_id, product_id, license_id, granted_at, source
		FROM digital_access_grants
		WHERE tenant_id = $1 AND customer_id = $2
		ORDER BY granted_at ASC
		LIMIT $3 OFFSET $4`, tenantID, customerID, perPage, (page-1)*perPage)
	if err != nil {
		return port.AccessGrantList{}, fmt.Errorf("list grants (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()
	grants := make([]digital.AccessGrant, 0)
	for rows.Next() {
		rec, scanErr := scanAccessGrantRow(rows)
		if scanErr != nil {
			return port.AccessGrantList{}, scanErr
		}
		grants = append(grants, digital.ReconstructAccessGrant(rec))
	}
	if rows.Err() != nil {
		return port.AccessGrantList{}, rows.Err()
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM digital_access_grants WHERE tenant_id = $1 AND customer_id = $2`, tenantID, customerID).Scan(&total); err != nil {
		return port.AccessGrantList{}, fmt.Errorf("count grants: %w", err)
	}
	return port.AccessGrantList{Grants: grants, Total: total}, nil
}

func (r *AccessGrantRepository) GetByCustomerProduct(ctx context.Context, tenantID string, customerID, productID uuid.UUID) (digital.AccessGrant, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return digital.AccessGrant{}, err
	}
	rec, err := scanAccessGrantRow(r.pool.QueryRow(ctx, `
		SELECT id, tenant_id, customer_id, product_id, license_id, granted_at, source
		FROM digital_access_grants
		WHERE tenant_id = $1 AND customer_id = $2 AND product_id = $3`,
		tenantID, customerID, productID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return digital.AccessGrant{}, port.ErrAccessGrantNotFound
		}
		return digital.AccessGrant{}, err
	}
	return digital.ReconstructAccessGrant(rec), nil
}

func scanAccessGrantRow(row pgx.Row) (digital.AccessGrantRecord, error) {
	var rec digital.AccessGrantRecord
	var source string
	if err := row.Scan(
		&rec.ID, &rec.TenantID, &rec.CustomerID, &rec.ProductID,
		&rec.LicenseID, &rec.GrantedAt, &source,
	); err != nil {
		return digital.AccessGrantRecord{}, err
	}
	parsed, err := digital.ParseSource(source)
	if err != nil {
		return digital.AccessGrantRecord{}, fmt.Errorf("scan access grant source %q: %w", source, err)
	}
	rec.Source = parsed
	return rec, nil
}
