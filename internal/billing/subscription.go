package billing

import (
	"strings"
	"time"
)

// Subscription is the per-tenant billing aggregate. The Stripe ids are
// optional in dev/in-memory runs but always populated when the Stripe
// adapter has provisioned a real customer + subscription.
type Subscription struct {
	ID                   string    `json:"id"`
	TenantID             string    `json:"tenant_id"`
	PlanID               string    `json:"plan_id"`
	State                State     `json:"state"`
	StripeSubscriptionID string    `json:"stripe_subscription_id,omitempty"`
	StripeCustomerID     string    `json:"stripe_customer_id,omitempty"`
	CurrentPeriodStart   time.Time `json:"current_period_start"`
	CurrentPeriodEnd     time.Time `json:"current_period_end"`
	CancelAtPeriodEnd    bool      `json:"cancel_at_period_end"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

// NewSubscriptionInput is the validated factory input for
// NewSubscription. Times default to UTC; State defaults to trialing.
type NewSubscriptionInput struct {
	ID                   string
	TenantID             string
	PlanID               string
	StripeSubscriptionID string
	StripeCustomerID     string
	State                State
	CurrentPeriodStart   time.Time
	CurrentPeriodEnd     time.Time
	Now                  time.Time
}

// NewSubscription returns a normalised Subscription value or an error
// when required fields are missing or the state is not parseable.
func NewSubscription(in NewSubscriptionInput) (Subscription, error) {
	if strings.TrimSpace(in.ID) == "" {
		return Subscription{}, ErrSubscriptionNotFound
	}
	if strings.TrimSpace(in.TenantID) == "" {
		return Subscription{}, ErrTenantRequired
	}
	if strings.TrimSpace(in.PlanID) == "" {
		return Subscription{}, ErrPlanNotFound
	}
	state := in.State
	if state == "" {
		state = StateTrialing
	}
	if _, err := ParseState(string(state)); err != nil {
		return Subscription{}, err
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	return Subscription{
		ID:                   in.ID,
		TenantID:             in.TenantID,
		PlanID:               in.PlanID,
		State:                state,
		StripeSubscriptionID: strings.TrimSpace(in.StripeSubscriptionID),
		StripeCustomerID:     strings.TrimSpace(in.StripeCustomerID),
		CurrentPeriodStart:   in.CurrentPeriodStart.UTC(),
		CurrentPeriodEnd:     in.CurrentPeriodEnd.UTC(),
		CreatedAt:            now,
		UpdatedAt:            now,
	}, nil
}

// Apply transitions the Subscription to a new state via the explicit
// transition table. Returns ErrInvalidTransition for illegal pairs.
// The caller is responsible for persisting the result.
func (s Subscription) Apply(t Transition, now time.Time) (Subscription, error) {
	target, err := nextState(s.State, t)
	if err != nil {
		return Subscription{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	s.State = target
	s.UpdatedAt = now.UTC()
	return s, nil
}

// InvoiceStatus is the lifecycle status of an Invoice.
type InvoiceStatus string

const (
	// InvoiceOpen is the initial status awaiting payment.
	InvoiceOpen InvoiceStatus = "open"
	// InvoicePaid is the success status set by invoice.payment_succeeded.
	InvoicePaid InvoiceStatus = "paid"
	// InvoiceVoid is set when the invoice is voided manually.
	InvoiceVoid InvoiceStatus = "void"
	// InvoiceUncollectible is set when retries are exhausted.
	InvoiceUncollectible InvoiceStatus = "uncollectible"
)

// String returns the canonical string for an InvoiceStatus.
func (s InvoiceStatus) String() string { return string(s) }

// Invoice is a tenant-scoped billing invoice. Money is stored in
// minor units to match the v2.2.0 / v2.3.0 conventions.
type Invoice struct {
	ID             string        `json:"id"`
	TenantID       string        `json:"tenant_id"`
	SubscriptionID string        `json:"subscription_id"`
	Amount         int           `json:"amount"`
	Currency       string        `json:"currency"`
	Status         InvoiceStatus `json:"status"`
	PeriodStart    time.Time     `json:"period_start"`
	PeriodEnd      time.Time     `json:"period_end"`
	CreatedAt      time.Time     `json:"created_at"`
	UpdatedAt      time.Time     `json:"updated_at"`
}

// UsageRecord is a single usage data point for a (tenant, metric,
// period). Persisted to the usage_records table; rollup happens via
// the UsageMeter port.
type UsageRecord struct {
	TenantID    string    `json:"tenant_id"`
	Metric      string    `json:"metric"`
	Value       int64     `json:"value"`
	PeriodStart time.Time `json:"period_start"`
	PeriodEnd   time.Time `json:"period_end"`
	RecordedAt  time.Time `json:"recorded_at"`
}

// Plan is the read-only catalog representation of a billing plan.
// The same plan_id is shared with Stripe so admins can correlate.
type Plan struct {
	ID                  string `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	StripePriceID       string `json:"stripe_price_id,omitempty"`
	APIRatePerMinute    int    `json:"api_rate_per_minute"`
	StorageBytes        int64  `json:"storage_bytes"`
	AgentRunsPerDay     int    `json:"agent_runs_per_day"`
	PluginCount         int    `json:"plugin_count"`
	PriceAmountMinor    int    `json:"price_amount_minor"`
	PriceCurrency       string `json:"price_currency"`
	BillingIntervalDays int    `json:"billing_interval_days"`
}
