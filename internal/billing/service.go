package billing

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// SubscriptionList is the paginated output of Service.ListSubscriptions.
type SubscriptionList struct {
	Subscriptions []Subscription `json:"subscriptions"`
	Total         int            `json:"total"`
}

// InvoiceList is the paginated output of Service.ListInvoices.
type InvoiceList struct {
	Invoices []Invoice `json:"invoices"`
	Total    int       `json:"total"`
}

// Repository persists subscriptions, invoices, and stripe webhook
// events. Every method is tenant-aware so adapters cannot accidentally
// cross tenant boundaries.
type Repository interface {
	CreateSubscription(ctx context.Context, sub Subscription) error
	GetSubscription(ctx context.Context, tenantID, id string) (Subscription, error)
	GetSubscriptionByStripeID(ctx context.Context, tenantID, stripeID string) (Subscription, error)
	ListSubscriptions(ctx context.Context, tenantID string, page, perPage int) (SubscriptionList, error)
	SaveSubscription(ctx context.Context, sub Subscription) error

	UpsertInvoice(ctx context.Context, inv Invoice) error
	GetInvoice(ctx context.Context, tenantID, id string) (Invoice, error)
	ListInvoices(ctx context.Context, tenantID string, page, perPage int) (InvoiceList, error)

	StripeEventSeen(ctx context.Context, eventID string) (bool, error)
	StripeEventRecord(ctx context.Context, eventID, eventType string, occurredAt time.Time) error
}

// PlanCatalog is a read-only port for plan lookups. Adapters can be
// hard-coded (in-memory) or backed by postgres.
type PlanCatalog interface {
	Get(ctx context.Context, planID string) (Plan, error)
	List(ctx context.Context) ([]Plan, error)
}

// EventPublisher publishes billing events to the in-process event bus.
// Each Stripe event handler emits a corresponding billing.* event so
// marketplace plugins can subscribe.
type EventPublisher interface {
	PublishBilling(ctx context.Context, eventType, source string, occurredAt time.Time, payload BillingPayload) error
}

// Service orchestrates billing CRUD and state transitions. It is the
// thing /api/v1/admin/billing/* and /webhooks/stripe handlers call.
type Service struct {
	repo  Repository
	plans PlanCatalog
	pub   EventPublisher
	now   func() time.Time
}

// ServiceConfig configures a Service.
type ServiceConfig struct {
	Repository Repository
	Plans      PlanCatalog
	Publisher  EventPublisher
	Now        func() time.Time
}

// NewService validates the configuration and returns a Service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Repository == nil {
		return nil, fmt.Errorf("billing service: repository required")
	}
	if cfg.Plans == nil {
		return nil, fmt.Errorf("billing service: plan catalog required")
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &Service{repo: cfg.Repository, plans: cfg.Plans, pub: cfg.Publisher, now: now}, nil
}

// CreateSubscription persists a new Subscription. The plan id must
// resolve in the catalog. Tenant id is required.
func (s *Service) CreateSubscription(ctx context.Context, in NewSubscriptionInput) (Subscription, error) {
	if _, err := s.plans.Get(ctx, in.PlanID); err != nil {
		return Subscription{}, err
	}
	if in.Now.IsZero() {
		in.Now = s.now()
	}
	sub, err := NewSubscription(in)
	if err != nil {
		return Subscription{}, err
	}
	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return Subscription{}, err
	}
	s.publish(ctx, "subscription.created", sub, "")
	return sub, nil
}

// GetSubscription returns the subscription with the given id.
func (s *Service) GetSubscription(ctx context.Context, tenantID, id string) (Subscription, error) {
	if tenantID == "" {
		return Subscription{}, ErrTenantRequired
	}
	return s.repo.GetSubscription(ctx, tenantID, id)
}

// ListSubscriptions returns a paginated list scoped to tenantID.
func (s *Service) ListSubscriptions(ctx context.Context, tenantID string, page, perPage int) (SubscriptionList, error) {
	if tenantID == "" {
		return SubscriptionList{}, ErrTenantRequired
	}
	return s.repo.ListSubscriptions(ctx, tenantID, page, perPage)
}

// Cancel transitions any non-terminal subscription state -> canceled.
func (s *Service) Cancel(ctx context.Context, tenantID, id string) (Subscription, error) {
	return s.transition(ctx, tenantID, id, TransitionCancel, "subscription.canceled")
}

// Pause transitions active -> paused.
func (s *Service) Pause(ctx context.Context, tenantID, id string) (Subscription, error) {
	return s.transition(ctx, tenantID, id, TransitionPause, "subscription.updated")
}

// Resume transitions paused -> active.
func (s *Service) Resume(ctx context.Context, tenantID, id string) (Subscription, error) {
	return s.transition(ctx, tenantID, id, TransitionResume, "subscription.updated")
}

// Activate transitions trialing|past_due -> active.
func (s *Service) Activate(ctx context.Context, tenantID, id string) (Subscription, error) {
	sub, err := s.repo.GetSubscription(ctx, tenantID, id)
	if err != nil {
		return Subscription{}, err
	}
	transition := TransitionActivate
	if sub.State == StatePastDue {
		transition = TransitionRecover
	}
	return s.transition(ctx, tenantID, id, transition, "subscription.updated")
}

// MarkPastDue is invoked by invoice.payment_failed.
func (s *Service) MarkPastDue(ctx context.Context, tenantID, id string) (Subscription, error) {
	return s.transition(ctx, tenantID, id, TransitionMarkPastDue, "subscription.updated")
}

func (s *Service) transition(ctx context.Context, tenantID, id string, t Transition, eventType string) (Subscription, error) {
	if tenantID == "" {
		return Subscription{}, ErrTenantRequired
	}
	sub, err := s.repo.GetSubscription(ctx, tenantID, id)
	if err != nil {
		return Subscription{}, err
	}
	updated, err := sub.Apply(t, s.now())
	if err != nil {
		return Subscription{}, err
	}
	if err := s.repo.SaveSubscription(ctx, updated); err != nil {
		return Subscription{}, err
	}
	s.publish(ctx, eventType, updated, "")
	return updated, nil
}

// UpsertSubscriptionFromStripe is the idempotent upsert used by the
// Stripe webhook handler when it sees a subscription.* event. The
// Stripe id is the deduplication key.
func (s *Service) UpsertSubscriptionFromStripe(ctx context.Context, in NewSubscriptionInput, eventType string) (Subscription, error) {
	if in.Now.IsZero() {
		in.Now = s.now()
	}
	if existing, err := s.repo.GetSubscriptionByStripeID(ctx, in.TenantID, in.StripeSubscriptionID); err == nil {
		updated := existing
		if in.State != "" {
			updated.State = in.State
		}
		if !in.CurrentPeriodEnd.IsZero() {
			updated.CurrentPeriodEnd = in.CurrentPeriodEnd.UTC()
		}
		if !in.CurrentPeriodStart.IsZero() {
			updated.CurrentPeriodStart = in.CurrentPeriodStart.UTC()
		}
		updated.UpdatedAt = s.now()
		if err := s.repo.SaveSubscription(ctx, updated); err != nil {
			return Subscription{}, err
		}
		s.publish(ctx, eventType, updated, "stripe")
		return updated, nil
	} else if !errors.Is(err, ErrSubscriptionNotFound) {
		return Subscription{}, err
	}
	sub, err := NewSubscription(in)
	if err != nil {
		return Subscription{}, err
	}
	if err := s.repo.CreateSubscription(ctx, sub); err != nil {
		return Subscription{}, err
	}
	s.publish(ctx, eventType, sub, "stripe")
	return sub, nil
}

// UpsertInvoice persists an invoice idempotently (by id) and emits
// an invoice.* event.
func (s *Service) UpsertInvoice(ctx context.Context, inv Invoice, eventType string) (Invoice, error) {
	if inv.TenantID == "" {
		return Invoice{}, ErrTenantRequired
	}
	if inv.Status == "" {
		inv.Status = InvoiceOpen
	}
	now := s.now()
	if inv.CreatedAt.IsZero() {
		inv.CreatedAt = now
	}
	inv.UpdatedAt = now
	if err := s.repo.UpsertInvoice(ctx, inv); err != nil {
		return Invoice{}, err
	}
	s.publish(ctx, eventType, Subscription{TenantID: inv.TenantID, ID: inv.SubscriptionID}, "stripe")
	return inv, nil
}

// ListInvoices returns paginated invoices for a tenant.
func (s *Service) ListInvoices(ctx context.Context, tenantID string, page, perPage int) (InvoiceList, error) {
	if tenantID == "" {
		return InvoiceList{}, ErrTenantRequired
	}
	return s.repo.ListInvoices(ctx, tenantID, page, perPage)
}

// GetInvoice returns one invoice by id.
func (s *Service) GetInvoice(ctx context.Context, tenantID, id string) (Invoice, error) {
	if tenantID == "" {
		return Invoice{}, ErrTenantRequired
	}
	return s.repo.GetInvoice(ctx, tenantID, id)
}

func (s *Service) publish(ctx context.Context, eventType string, sub Subscription, source string) {
	if s.pub == nil || eventType == "" {
		return
	}
	payload := BillingPayload{
		Version:              BillingPayloadVersion,
		TenantID:             sub.TenantID,
		SubscriptionID:       sub.ID,
		PlanID:               sub.PlanID,
		StripeSubscriptionID: sub.StripeSubscriptionID,
		State:                string(sub.State),
	}
	_ = s.pub.PublishBilling(ctx, eventType, source, s.now(), payload)
}
