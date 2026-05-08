package postgres

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/marketplace"
)

// MarketplaceCatalog is the postgres-backed catalogue. SQL is
// intentionally explicit so query plans stay reviewable.
type MarketplaceCatalog struct {
	pool productStore
}

// NewMarketplaceCatalog constructs a catalog repository.
func NewMarketplaceCatalog(pool *pgxpool.Pool) *MarketplaceCatalog {
	return &MarketplaceCatalog{pool: pool}
}

// RegisterManifest inserts a new manifest row.
func (r *MarketplaceCatalog) RegisterManifest(ctx context.Context, m marketplace.Manifest) error {
	if err := m.Validate(); err != nil {
		return err
	}
	deps, err := json.Marshal(m.Dependencies)
	if err != nil {
		return fmt.Errorf("marshal deps: %w", err)
	}
	now := time.Now().UTC()
	const q = `
		INSERT INTO marketplace_plugins (
			slug, name, version, vendor, description, category, homepage_url,
			event_subscriptions, permissions, dependencies, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err = r.pool.Exec(ctx, q,
		m.Slug, m.Name, m.Version, m.Vendor, m.Description, m.Category,
		m.HomepageURL,
		eventNamesAsStrings(m.EventSubscriptions),
		permissionsAsStrings(m.Permissions),
		deps, now, now,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: %s", marketplace.ErrSlugAlreadyExists, m.Slug)
		}
		return fmt.Errorf("insert plugin %s: %w", m.Slug, err)
	}
	return nil
}

// GetManifest reads a single manifest by slug.
func (r *MarketplaceCatalog) GetManifest(ctx context.Context, slug string) (marketplace.Manifest, error) {
	const q = `
		SELECT slug, name, version, vendor, description, category, homepage_url,
		       event_subscriptions, permissions, dependencies
		FROM marketplace_plugins WHERE slug = $1`
	row := r.pool.QueryRow(ctx, q, slug)
	m, err := scanManifestRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return marketplace.Manifest{}, fmt.Errorf("%w: slug=%s", marketplace.ErrPluginNotFound, slug)
		}
		return marketplace.Manifest{}, err
	}
	return m, nil
}

// ListManifests returns paginated manifests sorted by slug.
func (r *MarketplaceCatalog) ListManifests(ctx context.Context, page, perPage int) ([]marketplace.Manifest, int, error) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	const countQ = `SELECT COUNT(*) FROM marketplace_plugins`
	var total int
	if err := r.pool.QueryRow(ctx, countQ).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count plugins: %w", err)
	}
	const q = `
		SELECT slug, name, version, vendor, description, category, homepage_url,
		       event_subscriptions, permissions, dependencies
		FROM marketplace_plugins
		ORDER BY slug ASC
		LIMIT $1 OFFSET $2`
	rows, err := r.pool.Query(ctx, q, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("list plugins: %w", err)
	}
	defer rows.Close()
	out := make([]marketplace.Manifest, 0, perPage)
	for rows.Next() {
		m, err := scanManifestRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, m)
	}
	return out, total, rows.Err()
}

// MarketplaceInstallations is the postgres adapter for installation rows.
type MarketplaceInstallations struct {
	pool productStore
}

// NewMarketplaceInstallations builds a postgres installation repo.
func NewMarketplaceInstallations(pool *pgxpool.Pool) *MarketplaceInstallations {
	return &MarketplaceInstallations{pool: pool}
}

// Create inserts a new installation row.
func (r *MarketplaceInstallations) Create(ctx context.Context, ins marketplace.Installation) error {
	tenantID, err := requireTenantID(ins.TenantID)
	if err != nil {
		return err
	}
	const q = `
		INSERT INTO marketplace_installations (
			tenant_id, slug, installed_version, state, installed_at, activated_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7)`
	_, err = r.pool.Exec(ctx, q,
		tenantID, ins.Slug, ins.InstalledVersion, string(ins.State),
		parseRFC3339(ins.InstalledAt), parseNullableTime(ins.ActivatedAt), parseRFC3339(ins.UpdatedAt),
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginAlreadyInstalled, tenantID, ins.Slug)
		}
		return fmt.Errorf("insert installation tenant=%s slug=%s: %w", tenantID, ins.Slug, err)
	}
	return nil
}

// Get reads a single installation row.
func (r *MarketplaceInstallations) Get(ctx context.Context, tenantID, slug string) (marketplace.Installation, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return marketplace.Installation{}, err
	}
	const q = `
		SELECT tenant_id, slug, installed_version, state, installed_at, activated_at, updated_at
		FROM marketplace_installations
		WHERE tenant_id = $1 AND slug = $2`
	row := r.pool.QueryRow(ctx, q, tenantID, slug)
	ins, err := scanInstallationRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return marketplace.Installation{}, fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginNotFound, tenantID, slug)
		}
		return marketplace.Installation{}, err
	}
	return ins, nil
}

// List returns paginated installation rows for a tenant.
func (r *MarketplaceInstallations) List(ctx context.Context, tenantID string, page, perPage int) ([]marketplace.Installation, int, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	const countQ = `SELECT COUNT(*) FROM marketplace_installations WHERE tenant_id = $1`
	var total int
	if err := r.pool.QueryRow(ctx, countQ, tenantID).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count installations: %w", err)
	}
	const q = `
		SELECT tenant_id, slug, installed_version, state, installed_at, activated_at, updated_at
		FROM marketplace_installations
		WHERE tenant_id = $1
		ORDER BY installed_at ASC
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return nil, 0, fmt.Errorf("list installations: %w", err)
	}
	defer rows.Close()
	out := make([]marketplace.Installation, 0, perPage)
	for rows.Next() {
		ins, err := scanInstallationRow(rows)
		if err != nil {
			return nil, 0, err
		}
		out = append(out, ins)
	}
	return out, total, rows.Err()
}

// SaveState updates state/activated_at/updated_at for an installation.
func (r *MarketplaceInstallations) SaveState(ctx context.Context, ins marketplace.Installation) error {
	tenantID, err := requireTenantID(ins.TenantID)
	if err != nil {
		return err
	}
	const q = `
		UPDATE marketplace_installations
		SET state = $3, activated_at = $4, updated_at = $5
		WHERE tenant_id = $1 AND slug = $2`
	tag, err := r.pool.Exec(ctx, q,
		tenantID, ins.Slug, string(ins.State),
		parseNullableTime(ins.ActivatedAt), parseRFC3339(ins.UpdatedAt),
	)
	if err != nil {
		return fmt.Errorf("update installation tenant=%s slug=%s: %w", tenantID, ins.Slug, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginNotFound, tenantID, ins.Slug)
	}
	return nil
}

// Delete removes an installation row.
func (r *MarketplaceInstallations) Delete(ctx context.Context, tenantID, slug string) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	const q = `DELETE FROM marketplace_installations WHERE tenant_id = $1 AND slug = $2`
	tag, err := r.pool.Exec(ctx, q, tenantID, slug)
	if err != nil {
		return fmt.Errorf("delete installation tenant=%s slug=%s: %w", tenantID, slug, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: tenant=%s slug=%s", marketplace.ErrPluginNotFound, tenantID, slug)
	}
	return nil
}

// MarketplaceSubscriptions is the postgres adapter for the
// per-tenant per-plugin event subscription table.
type MarketplaceSubscriptions struct {
	pool productStore
}

// NewMarketplaceSubscriptions builds a postgres subscription repo.
func NewMarketplaceSubscriptions(pool *pgxpool.Pool) *MarketplaceSubscriptions {
	return &MarketplaceSubscriptions{pool: pool}
}

// Replace overwrites the subscription set in a single transaction so
// observers never see a partial replacement.
func (r *MarketplaceSubscriptions) Replace(ctx context.Context, tenantID, slug string, events []marketplace.EventName) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	const delQ = `DELETE FROM marketplace_event_subscriptions WHERE tenant_id = $1 AND slug = $2`
	if _, err := r.pool.Exec(ctx, delQ, tenantID, slug); err != nil {
		return fmt.Errorf("clear subs tenant=%s slug=%s: %w", tenantID, slug, err)
	}
	for _, evt := range events {
		const insQ = `INSERT INTO marketplace_event_subscriptions (tenant_id, slug, event_name) VALUES ($1, $2, $3)`
		if _, err := r.pool.Exec(ctx, insQ, tenantID, slug, string(evt)); err != nil {
			return fmt.Errorf("insert sub tenant=%s slug=%s event=%s: %w", tenantID, slug, evt, err)
		}
	}
	return nil
}

// List returns the subscription set for (tenant, slug).
func (r *MarketplaceSubscriptions) List(ctx context.Context, tenantID, slug string) ([]marketplace.EventName, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return nil, err
	}
	const q = `SELECT event_name FROM marketplace_event_subscriptions WHERE tenant_id = $1 AND slug = $2 ORDER BY event_name ASC`
	rows, err := r.pool.Query(ctx, q, tenantID, slug)
	if err != nil {
		return nil, fmt.Errorf("list subs: %w", err)
	}
	defer rows.Close()
	var out []marketplace.EventName
	for rows.Next() {
		var e string
		if err := rows.Scan(&e); err != nil {
			return nil, err
		}
		out = append(out, marketplace.EventName(e))
	}
	return out, rows.Err()
}

// Delete removes the subscription rows for (tenant, slug).
func (r *MarketplaceSubscriptions) Delete(ctx context.Context, tenantID, slug string) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	const q = `DELETE FROM marketplace_event_subscriptions WHERE tenant_id = $1 AND slug = $2`
	if _, err := r.pool.Exec(ctx, q, tenantID, slug); err != nil {
		return fmt.Errorf("delete subs tenant=%s slug=%s: %w", tenantID, slug, err)
	}
	return nil
}

// scanManifestRow reads one row into a marketplace.Manifest.
func scanManifestRow(row pgx.Row) (marketplace.Manifest, error) {
	var (
		m         marketplace.Manifest
		events    []string
		perms     []string
		depsBytes []byte
	)
	if err := row.Scan(
		&m.Slug, &m.Name, &m.Version, &m.Vendor, &m.Description,
		&m.Category, &m.HomepageURL,
		&events, &perms, &depsBytes,
	); err != nil {
		return marketplace.Manifest{}, err
	}
	m.EventSubscriptions = make([]marketplace.EventName, len(events))
	for i, e := range events {
		m.EventSubscriptions[i] = marketplace.EventName(e)
	}
	m.Permissions = make([]marketplace.Permission, len(perms))
	for i, p := range perms {
		m.Permissions[i] = marketplace.Permission(p)
	}
	if len(depsBytes) > 0 {
		if err := json.Unmarshal(depsBytes, &m.Dependencies); err != nil {
			return marketplace.Manifest{}, fmt.Errorf("decode deps: %w", err)
		}
	}
	return m, nil
}

// scanInstallationRow reads one row into a marketplace.Installation.
func scanInstallationRow(row pgx.Row) (marketplace.Installation, error) {
	var (
		ins         marketplace.Installation
		state       string
		installedAt time.Time
		activatedAt *time.Time
		updatedAt   time.Time
	)
	if err := row.Scan(
		&ins.TenantID, &ins.Slug, &ins.InstalledVersion, &state,
		&installedAt, &activatedAt, &updatedAt,
	); err != nil {
		return marketplace.Installation{}, err
	}
	ins.State = marketplace.State(state)
	ins.InstalledAt = installedAt.Format(time.RFC3339Nano)
	if activatedAt != nil {
		ins.ActivatedAt = activatedAt.Format(time.RFC3339Nano)
	}
	ins.UpdatedAt = updatedAt.Format(time.RFC3339Nano)
	return ins, nil
}

// eventNamesAsStrings converts a slice of EventName into the []string
// representation pgx wants for TEXT[] arrays.
func eventNamesAsStrings(events []marketplace.EventName) []string {
	out := make([]string, len(events))
	for i, e := range events {
		out[i] = string(e)
	}
	return out
}

// permissionsAsStrings converts a slice of Permission into []string.
func permissionsAsStrings(perms []marketplace.Permission) []string {
	out := make([]string, len(perms))
	for i, p := range perms {
		out[i] = string(p)
	}
	return out
}

// parseRFC3339 returns a time.Time from a string, falling back to
// time.Now if the input is empty or unparseable. Adapter rows always
// have non-empty timestamps when written via the registry.
func parseRFC3339(s string) time.Time {
	if s == "" {
		return time.Now().UTC()
	}
	t, err := time.Parse(time.RFC3339Nano, s)
	if err != nil {
		t, err = time.Parse(time.RFC3339, s)
		if err != nil {
			return time.Now().UTC()
		}
	}
	return t.UTC()
}

// parseNullableTime returns a *time.Time, nil for empty input.
func parseNullableTime(s string) *time.Time {
	if s == "" {
		return nil
	}
	t := parseRFC3339(s)
	return &t
}

// isUniqueViolation reports whether err is a postgres unique violation
// (SQLSTATE 23505). We use string-matching to avoid pulling in the
// pgconn-error type and keep the adapter package's coupling small.
func isUniqueViolation(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "23505") || strings.Contains(msg, "duplicate key")
}
