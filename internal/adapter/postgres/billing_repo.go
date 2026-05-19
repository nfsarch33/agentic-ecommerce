package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/helixon-ec/internal/billing"
)

// BillingRepository is the postgres adapter for the v2.5.0 billing
// bounded context. SQL is intentionally explicit so query plans stay
// reviewable.
type BillingRepository struct {
	pool productStore
}

// NewBillingRepository constructs a billing repository backed by pool.
func NewBillingRepository(pool *pgxpool.Pool) *BillingRepository {
	return &BillingRepository{pool: pool}
}

// CreateSubscription inserts a new billing_subscriptions row.
func (r *BillingRepository) CreateSubscription(ctx context.Context, sub billing.Subscription) error {
	if sub.TenantID == "" {
		return billing.ErrTenantRequired
	}
	const q = `
		INSERT INTO billing_subscriptions (
			id, tenant_id, plan_id, state, stripe_subscription_id, stripe_customer_id,
			current_period_start, current_period_end, cancel_at_period_end,
			created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err := r.pool.Exec(ctx, q,
		sub.ID, sub.TenantID, sub.PlanID, string(sub.State),
		sub.StripeSubscriptionID, sub.StripeCustomerID,
		nullTime(sub.CurrentPeriodStart), nullTime(sub.CurrentPeriodEnd),
		sub.CancelAtPeriodEnd, sub.CreatedAt, sub.UpdatedAt,
	)
	if err != nil {
		if isUniqueViolation(err) {
			return fmt.Errorf("%w: id=%s", billing.ErrSubscriptionAlreadyExists, sub.ID)
		}
		return fmt.Errorf("insert billing_subscription %s: %w", sub.ID, err)
	}
	return nil
}

// GetSubscription returns the row for (tenant, id).
func (r *BillingRepository) GetSubscription(ctx context.Context, tenantID, id string) (billing.Subscription, error) {
	if tenantID == "" {
		return billing.Subscription{}, billing.ErrTenantRequired
	}
	const q = `
		SELECT id, tenant_id, plan_id, state, stripe_subscription_id, stripe_customer_id,
		       current_period_start, current_period_end, cancel_at_period_end,
		       created_at, updated_at
		FROM billing_subscriptions
		WHERE tenant_id = $1 AND id = $2`
	row := r.pool.QueryRow(ctx, q, tenantID, id)
	sub, err := scanSubscription(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return billing.Subscription{}, fmt.Errorf("%w: tenant=%s id=%s", billing.ErrSubscriptionNotFound, tenantID, id)
		}
		return billing.Subscription{}, err
	}
	return sub, nil
}

// GetSubscriptionByStripeID returns the row matching tenant + stripe id.
func (r *BillingRepository) GetSubscriptionByStripeID(ctx context.Context, tenantID, stripeID string) (billing.Subscription, error) {
	if tenantID == "" {
		return billing.Subscription{}, billing.ErrTenantRequired
	}
	const q = `
		SELECT id, tenant_id, plan_id, state, stripe_subscription_id, stripe_customer_id,
		       current_period_start, current_period_end, cancel_at_period_end,
		       created_at, updated_at
		FROM billing_subscriptions
		WHERE tenant_id = $1 AND stripe_subscription_id = $2`
	row := r.pool.QueryRow(ctx, q, tenantID, stripeID)
	sub, err := scanSubscription(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return billing.Subscription{}, fmt.Errorf("%w: tenant=%s stripe_id=%s", billing.ErrSubscriptionNotFound, tenantID, stripeID)
		}
		return billing.Subscription{}, err
	}
	return sub, nil
}

// ListSubscriptions returns paginated rows scoped to tenant.
func (r *BillingRepository) ListSubscriptions(ctx context.Context, tenantID string, page, perPage int) (billing.SubscriptionList, error) {
	if tenantID == "" {
		return billing.SubscriptionList{}, billing.ErrTenantRequired
	}
	page, perPage = normalize(page, perPage)
	const countQ = `SELECT COUNT(*) FROM billing_subscriptions WHERE tenant_id = $1`
	var total int
	if err := r.pool.QueryRow(ctx, countQ, tenantID).Scan(&total); err != nil {
		return billing.SubscriptionList{}, fmt.Errorf("count subscriptions: %w", err)
	}
	const q = `
		SELECT id, tenant_id, plan_id, state, stripe_subscription_id, stripe_customer_id,
		       current_period_start, current_period_end, cancel_at_period_end,
		       created_at, updated_at
		FROM billing_subscriptions
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return billing.SubscriptionList{}, fmt.Errorf("list subscriptions: %w", err)
	}
	defer rows.Close()
	out := make([]billing.Subscription, 0, perPage)
	for rows.Next() {
		sub, err := scanSubscription(rows)
		if err != nil {
			return billing.SubscriptionList{}, err
		}
		out = append(out, sub)
	}
	return billing.SubscriptionList{Subscriptions: out, Total: total}, rows.Err()
}

// SaveSubscription persists state, stripe ids, periods, and updated_at.
// Returns ErrSubscriptionNotFound when the row is missing.
func (r *BillingRepository) SaveSubscription(ctx context.Context, sub billing.Subscription) error {
	if sub.TenantID == "" {
		return billing.ErrTenantRequired
	}
	const q = `
		UPDATE billing_subscriptions
		SET plan_id = $3,
		    state = $4,
		    stripe_subscription_id = $5,
		    stripe_customer_id = $6,
		    current_period_start = $7,
		    current_period_end = $8,
		    cancel_at_period_end = $9,
		    updated_at = $10
		WHERE tenant_id = $1 AND id = $2`
	tag, err := r.pool.Exec(ctx, q,
		sub.TenantID, sub.ID, sub.PlanID, string(sub.State),
		sub.StripeSubscriptionID, sub.StripeCustomerID,
		nullTime(sub.CurrentPeriodStart), nullTime(sub.CurrentPeriodEnd),
		sub.CancelAtPeriodEnd, sub.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("save subscription %s: %w", sub.ID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("%w: tenant=%s id=%s", billing.ErrSubscriptionNotFound, sub.TenantID, sub.ID)
	}
	return nil
}

// UpsertInvoice upserts an invoice row by (tenant, id). Idempotent.
func (r *BillingRepository) UpsertInvoice(ctx context.Context, inv billing.Invoice) error {
	if inv.TenantID == "" {
		return billing.ErrTenantRequired
	}
	const q = `
		INSERT INTO billing_invoices (
			id, tenant_id, subscription_id, amount, currency, status,
			period_start, period_end, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (tenant_id, id) DO UPDATE SET
			subscription_id = EXCLUDED.subscription_id,
			amount = EXCLUDED.amount,
			currency = EXCLUDED.currency,
			status = EXCLUDED.status,
			period_start = EXCLUDED.period_start,
			period_end = EXCLUDED.period_end,
			updated_at = EXCLUDED.updated_at`
	_, err := r.pool.Exec(ctx, q,
		inv.ID, inv.TenantID, inv.SubscriptionID, inv.Amount, inv.Currency, string(inv.Status),
		nullTime(inv.PeriodStart), nullTime(inv.PeriodEnd),
		inv.CreatedAt, inv.UpdatedAt,
	)
	if err != nil {
		return fmt.Errorf("upsert invoice %s: %w", inv.ID, err)
	}
	return nil
}

// GetInvoice returns the invoice for (tenant, id).
func (r *BillingRepository) GetInvoice(ctx context.Context, tenantID, id string) (billing.Invoice, error) {
	if tenantID == "" {
		return billing.Invoice{}, billing.ErrTenantRequired
	}
	const q = `
		SELECT id, tenant_id, subscription_id, amount, currency, status,
		       period_start, period_end, created_at, updated_at
		FROM billing_invoices
		WHERE tenant_id = $1 AND id = $2`
	row := r.pool.QueryRow(ctx, q, tenantID, id)
	inv, err := scanInvoice(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return billing.Invoice{}, fmt.Errorf("%w: tenant=%s id=%s", billing.ErrInvoiceNotFound, tenantID, id)
		}
		return billing.Invoice{}, err
	}
	return inv, nil
}

// ListInvoices returns paginated invoices scoped to tenant.
func (r *BillingRepository) ListInvoices(ctx context.Context, tenantID string, page, perPage int) (billing.InvoiceList, error) {
	if tenantID == "" {
		return billing.InvoiceList{}, billing.ErrTenantRequired
	}
	page, perPage = normalize(page, perPage)
	const countQ = `SELECT COUNT(*) FROM billing_invoices WHERE tenant_id = $1`
	var total int
	if err := r.pool.QueryRow(ctx, countQ, tenantID).Scan(&total); err != nil {
		return billing.InvoiceList{}, fmt.Errorf("count invoices: %w", err)
	}
	const q = `
		SELECT id, tenant_id, subscription_id, amount, currency, status,
		       period_start, period_end, created_at, updated_at
		FROM billing_invoices
		WHERE tenant_id = $1
		ORDER BY created_at DESC
		LIMIT $2 OFFSET $3`
	rows, err := r.pool.Query(ctx, q, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return billing.InvoiceList{}, fmt.Errorf("list invoices: %w", err)
	}
	defer rows.Close()
	out := make([]billing.Invoice, 0, perPage)
	for rows.Next() {
		inv, err := scanInvoice(rows)
		if err != nil {
			return billing.InvoiceList{}, err
		}
		out = append(out, inv)
	}
	return billing.InvoiceList{Invoices: out, Total: total}, rows.Err()
}

// StripeEventSeen reports whether the event_id has been processed.
func (r *BillingRepository) StripeEventSeen(ctx context.Context, eventID string) (bool, error) {
	const q = `SELECT 1 FROM stripe_webhook_events WHERE event_id = $1`
	var one int
	if err := r.pool.QueryRow(ctx, q, eventID).Scan(&one); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, nil
		}
		return false, fmt.Errorf("seen event %s: %w", eventID, err)
	}
	return one == 1, nil
}

// StripeEventRecord persists the (event_id, type, occurred_at) row.
// Idempotent on event_id.
func (r *BillingRepository) StripeEventRecord(ctx context.Context, eventID, eventType string, occurredAt time.Time) error {
	if eventID == "" {
		return fmt.Errorf("stripe event id required")
	}
	const q = `
		INSERT INTO stripe_webhook_events (event_id, event_type, occurred_at)
		VALUES ($1, $2, $3)
		ON CONFLICT (event_id) DO NOTHING`
	if _, err := r.pool.Exec(ctx, q, eventID, eventType, occurredAt.UTC()); err != nil {
		return fmt.Errorf("record event %s: %w", eventID, err)
	}
	return nil
}

func scanSubscription(row pgx.Row) (billing.Subscription, error) {
	var (
		sub                    billing.Subscription
		state                  string
		periodStart, periodEnd *time.Time
	)
	if err := row.Scan(
		&sub.ID, &sub.TenantID, &sub.PlanID, &state,
		&sub.StripeSubscriptionID, &sub.StripeCustomerID,
		&periodStart, &periodEnd, &sub.CancelAtPeriodEnd,
		&sub.CreatedAt, &sub.UpdatedAt,
	); err != nil {
		return billing.Subscription{}, err
	}
	parsedState, err := billing.ParseState(state)
	if err != nil {
		return billing.Subscription{}, fmt.Errorf("scan subscription %s: %w", sub.ID, err)
	}
	sub.State = parsedState
	if periodStart != nil {
		sub.CurrentPeriodStart = periodStart.UTC()
	}
	if periodEnd != nil {
		sub.CurrentPeriodEnd = periodEnd.UTC()
	}
	return sub, nil
}

func scanInvoice(row pgx.Row) (billing.Invoice, error) {
	var (
		inv                    billing.Invoice
		status                 string
		periodStart, periodEnd *time.Time
	)
	if err := row.Scan(
		&inv.ID, &inv.TenantID, &inv.SubscriptionID,
		&inv.Amount, &inv.Currency, &status,
		&periodStart, &periodEnd, &inv.CreatedAt, &inv.UpdatedAt,
	); err != nil {
		return billing.Invoice{}, err
	}
	parsed, err := billing.ParseInvoiceStatus(status)
	if err != nil {
		return billing.Invoice{}, fmt.Errorf("scan invoice %s: %w", inv.ID, err)
	}
	inv.Status = parsed
	if periodStart != nil {
		inv.PeriodStart = periodStart.UTC()
	}
	if periodEnd != nil {
		inv.PeriodEnd = periodEnd.UTC()
	}
	return inv, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

func normalize(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 {
		perPage = 20
	}
	if perPage > 100 {
		perPage = 100
	}
	return page, perPage
}
