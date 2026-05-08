package billing

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// InMemoryRepository is the goroutine-safe in-process Repository
// implementation used by tests and dev mode. Production wiring uses
// internal/adapter/postgres.BillingRepository.
type InMemoryRepository struct {
	mu        sync.RWMutex
	subs      map[string]map[string]Subscription // tenant -> id -> subscription
	subStripe map[string]map[string]string       // tenant -> stripe id -> id
	invoices  map[string]map[string]Invoice      // tenant -> id -> invoice
	events    map[string]stripeEventRow          // event_id -> row
}

type stripeEventRow struct {
	EventID    string
	EventType  string
	OccurredAt time.Time
}

// NewInMemoryRepository returns an empty in-memory repo.
func NewInMemoryRepository() *InMemoryRepository {
	return &InMemoryRepository{
		subs:      make(map[string]map[string]Subscription),
		subStripe: make(map[string]map[string]string),
		invoices:  make(map[string]map[string]Invoice),
		events:    make(map[string]stripeEventRow),
	}
}

// CreateSubscription inserts a new subscription. ErrSubscriptionAlreadyExists
// when (tenant, id) already exists.
func (r *InMemoryRepository) CreateSubscription(_ context.Context, sub Subscription) error {
	if sub.TenantID == "" {
		return ErrTenantRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subs[sub.TenantID][sub.ID]; ok {
		return fmt.Errorf("%w: id=%q", ErrSubscriptionAlreadyExists, sub.ID)
	}
	if r.subs[sub.TenantID] == nil {
		r.subs[sub.TenantID] = make(map[string]Subscription)
	}
	r.subs[sub.TenantID][sub.ID] = sub
	if sub.StripeSubscriptionID != "" {
		if r.subStripe[sub.TenantID] == nil {
			r.subStripe[sub.TenantID] = make(map[string]string)
		}
		r.subStripe[sub.TenantID][sub.StripeSubscriptionID] = sub.ID
	}
	return nil
}

// GetSubscription returns the row for (tenant, id).
func (r *InMemoryRepository) GetSubscription(_ context.Context, tenantID, id string) (Subscription, error) {
	if tenantID == "" {
		return Subscription{}, ErrTenantRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, ok := r.subs[tenantID]
	if !ok {
		return Subscription{}, fmt.Errorf("%w: tenant=%q id=%q", ErrSubscriptionNotFound, tenantID, id)
	}
	sub, ok := rows[id]
	if !ok {
		return Subscription{}, fmt.Errorf("%w: tenant=%q id=%q", ErrSubscriptionNotFound, tenantID, id)
	}
	return sub, nil
}

// GetSubscriptionByStripeID looks up a subscription by its Stripe id.
func (r *InMemoryRepository) GetSubscriptionByStripeID(_ context.Context, tenantID, stripeID string) (Subscription, error) {
	if tenantID == "" {
		return Subscription{}, ErrTenantRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, ok := r.subStripe[tenantID]
	if !ok {
		return Subscription{}, fmt.Errorf("%w: tenant=%q stripe_id=%q", ErrSubscriptionNotFound, tenantID, stripeID)
	}
	id, ok := rows[stripeID]
	if !ok {
		return Subscription{}, fmt.Errorf("%w: tenant=%q stripe_id=%q", ErrSubscriptionNotFound, tenantID, stripeID)
	}
	return r.subs[tenantID][id], nil
}

// ListSubscriptions returns paginated rows sorted by created_at desc.
func (r *InMemoryRepository) ListSubscriptions(_ context.Context, tenantID string, page, perPage int) (SubscriptionList, error) {
	if tenantID == "" {
		return SubscriptionList{}, ErrTenantRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := r.subs[tenantID]
	out := make([]Subscription, 0, len(rows))
	for _, sub := range rows {
		out = append(out, sub)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	page, perPage = normalizePagination(page, perPage)
	total := len(out)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pageRows := make([]Subscription, end-start)
	copy(pageRows, out[start:end])
	return SubscriptionList{Subscriptions: pageRows, Total: total}, nil
}

// SaveSubscription persists an updated row. Errors when the row is missing.
func (r *InMemoryRepository) SaveSubscription(_ context.Context, sub Subscription) error {
	if sub.TenantID == "" {
		return ErrTenantRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	rows, ok := r.subs[sub.TenantID]
	if !ok {
		return fmt.Errorf("%w: tenant=%q id=%q", ErrSubscriptionNotFound, sub.TenantID, sub.ID)
	}
	if _, ok := rows[sub.ID]; !ok {
		return fmt.Errorf("%w: tenant=%q id=%q", ErrSubscriptionNotFound, sub.TenantID, sub.ID)
	}
	rows[sub.ID] = sub
	if sub.StripeSubscriptionID != "" {
		if r.subStripe[sub.TenantID] == nil {
			r.subStripe[sub.TenantID] = make(map[string]string)
		}
		r.subStripe[sub.TenantID][sub.StripeSubscriptionID] = sub.ID
	}
	return nil
}

// UpsertInvoice upserts an invoice by (tenant, id). Idempotent.
func (r *InMemoryRepository) UpsertInvoice(_ context.Context, inv Invoice) error {
	if inv.TenantID == "" {
		return ErrTenantRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.invoices[inv.TenantID] == nil {
		r.invoices[inv.TenantID] = make(map[string]Invoice)
	}
	if existing, ok := r.invoices[inv.TenantID][inv.ID]; ok {
		if !existing.CreatedAt.IsZero() {
			inv.CreatedAt = existing.CreatedAt
		}
	}
	r.invoices[inv.TenantID][inv.ID] = inv
	return nil
}

// GetInvoice returns the invoice for (tenant, id).
func (r *InMemoryRepository) GetInvoice(_ context.Context, tenantID, id string) (Invoice, error) {
	if tenantID == "" {
		return Invoice{}, ErrTenantRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows, ok := r.invoices[tenantID]
	if !ok {
		return Invoice{}, fmt.Errorf("%w: tenant=%q id=%q", ErrInvoiceNotFound, tenantID, id)
	}
	inv, ok := rows[id]
	if !ok {
		return Invoice{}, fmt.Errorf("%w: tenant=%q id=%q", ErrInvoiceNotFound, tenantID, id)
	}
	return inv, nil
}

// ListInvoices returns paginated invoices sorted by created_at desc.
func (r *InMemoryRepository) ListInvoices(_ context.Context, tenantID string, page, perPage int) (InvoiceList, error) {
	if tenantID == "" {
		return InvoiceList{}, ErrTenantRequired
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	rows := r.invoices[tenantID]
	out := make([]Invoice, 0, len(rows))
	for _, inv := range rows {
		out = append(out, inv)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	page, perPage = normalizePagination(page, perPage)
	total := len(out)
	start := (page - 1) * perPage
	if start > total {
		start = total
	}
	end := start + perPage
	if end > total {
		end = total
	}
	pageRows := make([]Invoice, end-start)
	copy(pageRows, out[start:end])
	return InvoiceList{Invoices: pageRows, Total: total}, nil
}

// StripeEventSeen reports whether event_id has already been processed.
func (r *InMemoryRepository) StripeEventSeen(_ context.Context, eventID string) (bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	_, ok := r.events[eventID]
	return ok, nil
}

// StripeEventRecord persists the (event_id, type, occurred_at) row.
func (r *InMemoryRepository) StripeEventRecord(_ context.Context, eventID, eventType string, occurredAt time.Time) error {
	if eventID == "" {
		return fmt.Errorf("stripe event id required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events[eventID] = stripeEventRow{EventID: eventID, EventType: eventType, OccurredAt: occurredAt.UTC()}
	return nil
}

func normalizePagination(page, perPage int) (int, int) {
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

// StaticPlanCatalog is a hard-coded PlanCatalog used by dev mode and
// tests. Production wiring loads from postgres.
type StaticPlanCatalog struct {
	plans map[string]Plan
}

// NewStaticPlanCatalog returns the seed catalog with three plans.
func NewStaticPlanCatalog(plans ...Plan) *StaticPlanCatalog {
	if len(plans) == 0 {
		plans = DefaultSeedPlans()
	}
	by := make(map[string]Plan, len(plans))
	for _, p := range plans {
		by[p.ID] = p
	}
	return &StaticPlanCatalog{plans: by}
}

// Get implements PlanCatalog.
func (c *StaticPlanCatalog) Get(_ context.Context, planID string) (Plan, error) {
	p, ok := c.plans[planID]
	if !ok {
		return Plan{}, fmt.Errorf("%w: plan_id=%q", ErrPlanNotFound, planID)
	}
	return p, nil
}

// List implements PlanCatalog. Order is deterministic by plan id.
func (c *StaticPlanCatalog) List(_ context.Context) ([]Plan, error) {
	ids := make([]string, 0, len(c.plans))
	for id := range c.plans {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	out := make([]Plan, 0, len(ids))
	for _, id := range ids {
		out = append(out, c.plans[id])
	}
	return out, nil
}

// DefaultSeedPlans returns the v2.5.0 baseline plans (Free, Starter,
// Pro). Limits are deliberately conservative so quota gates fire in
// tests.
func DefaultSeedPlans() []Plan {
	return []Plan{
		{
			ID:                  "free",
			Name:                "Free",
			Description:         "Hobby tier with strict limits.",
			APIRatePerMinute:    60,
			StorageBytes:        50 * 1024 * 1024,
			AgentRunsPerDay:     20,
			PluginCount:         3,
			PriceAmountMinor:    0,
			PriceCurrency:       "AUD",
			BillingIntervalDays: 30,
		},
		{
			ID:                  "starter",
			Name:                "Starter",
			Description:         "Solo founder tier.",
			StripePriceID:       "price_starter",
			APIRatePerMinute:    300,
			StorageBytes:        2 * 1024 * 1024 * 1024,
			AgentRunsPerDay:     500,
			PluginCount:         15,
			PriceAmountMinor:    1900,
			PriceCurrency:       "AUD",
			BillingIntervalDays: 30,
		},
		{
			ID:                  "pro",
			Name:                "Pro",
			Description:         "Growing team tier.",
			StripePriceID:       "price_pro",
			APIRatePerMinute:    1200,
			StorageBytes:        20 * 1024 * 1024 * 1024,
			AgentRunsPerDay:     5000,
			PluginCount:         50,
			PriceAmountMinor:    7900,
			PriceCurrency:       "AUD",
			BillingIntervalDays: 30,
		},
	}
}
