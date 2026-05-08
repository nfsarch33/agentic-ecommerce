package billing

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Stripe event types we care about. Centralising them as constants
// gives the dispatcher a small typed switch instead of magic strings.
const (
	StripeSubscriptionCreated     = "customer.subscription.created"
	StripeSubscriptionUpdated     = "customer.subscription.updated"
	StripeSubscriptionDeleted     = "customer.subscription.deleted"
	StripeInvoicePaymentSucceeded = "invoice.payment_succeeded"
	StripeInvoicePaymentFailed    = "invoice.payment_failed"
)

// stripeEventEnvelope is the minimal shape of a Stripe webhook event
// payload. We only decode the fields we actually use.
type stripeEventEnvelope struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Created int64           `json:"created"`
	Data    json.RawMessage `json:"data"`
}

// stripeEventData wraps the data.object field in a Stripe event.
type stripeEventData struct {
	Object json.RawMessage `json:"object"`
}

// stripeSubscriptionObject is the Stripe subscription resource shape
// we consume. Fields we don't use are omitted so the JSON decoder is
// strict about the surface area we depend on.
type stripeSubscriptionObject struct {
	ID                 string `json:"id"`
	Status             string `json:"status"`
	Customer           string `json:"customer"`
	CurrentPeriodStart int64  `json:"current_period_start"`
	CurrentPeriodEnd   int64  `json:"current_period_end"`
	Metadata           struct {
		TenantID string `json:"tenant_id"`
		PlanID   string `json:"plan_id"`
	} `json:"metadata"`
	CancelAtPeriodEnd bool `json:"cancel_at_period_end"`
}

// stripeInvoiceObject is the Stripe invoice resource shape we
// consume.
type stripeInvoiceObject struct {
	ID           string `json:"id"`
	Subscription string `json:"subscription"`
	Customer     string `json:"customer"`
	AmountDue    int    `json:"amount_due"`
	AmountPaid   int    `json:"amount_paid"`
	Currency     string `json:"currency"`
	Status       string `json:"status"`
	PeriodStart  int64  `json:"period_start"`
	PeriodEnd    int64  `json:"period_end"`
	Metadata     struct {
		TenantID string `json:"tenant_id"`
		PlanID   string `json:"plan_id"`
	} `json:"metadata"`
}

// Dispatcher routes a verified Stripe webhook payload to the right
// Service method. It is the single side-effecting handler called by
// /webhooks/stripe after signature verification.
type Dispatcher struct {
	svc *Service
}

// NewDispatcher returns a Dispatcher backed by svc.
func NewDispatcher(svc *Service) *Dispatcher {
	return &Dispatcher{svc: svc}
}

// Dispatch decodes the verified payload and applies the resulting
// state change. Idempotency on event_id is handled by the caller via
// Service.Repository.StripeEventSeen / StripeEventRecord.
func (d *Dispatcher) Dispatch(ctx context.Context, payload []byte) (string, error) {
	if d == nil || d.svc == nil {
		return "", fmt.Errorf("billing dispatcher: not configured")
	}
	var env stripeEventEnvelope
	if err := json.Unmarshal(payload, &env); err != nil {
		return "", fmt.Errorf("decode stripe event: %w", err)
	}
	if env.ID == "" {
		return "", fmt.Errorf("stripe event missing id")
	}
	switch env.Type {
	case StripeSubscriptionCreated, StripeSubscriptionUpdated:
		return env.ID, d.handleSubscriptionUpsert(ctx, env)
	case StripeSubscriptionDeleted:
		return env.ID, d.handleSubscriptionDeleted(ctx, env)
	case StripeInvoicePaymentSucceeded:
		return env.ID, d.handleInvoicePaid(ctx, env)
	case StripeInvoicePaymentFailed:
		return env.ID, d.handleInvoiceFailed(ctx, env)
	default:
		return env.ID, nil
	}
}

func (d *Dispatcher) handleSubscriptionUpsert(ctx context.Context, env stripeEventEnvelope) error {
	sub, err := decodeStripeSubscription(env.Data)
	if err != nil {
		return err
	}
	state := mapStripeStatus(sub.Status)
	in := NewSubscriptionInput{
		ID:                   sub.ID,
		TenantID:             sub.Metadata.TenantID,
		PlanID:               sub.Metadata.PlanID,
		StripeSubscriptionID: sub.ID,
		StripeCustomerID:     sub.Customer,
		State:                state,
		CurrentPeriodStart:   time.Unix(sub.CurrentPeriodStart, 0).UTC(),
		CurrentPeriodEnd:     time.Unix(sub.CurrentPeriodEnd, 0).UTC(),
	}
	if in.TenantID == "" {
		return fmt.Errorf("stripe subscription %s missing metadata.tenant_id", sub.ID)
	}
	eventType := "subscription.updated"
	if env.Type == StripeSubscriptionCreated {
		eventType = "subscription.created"
	}
	_, err = d.svc.UpsertSubscriptionFromStripe(ctx, in, eventType)
	return err
}

func (d *Dispatcher) handleSubscriptionDeleted(ctx context.Context, env stripeEventEnvelope) error {
	sub, err := decodeStripeSubscription(env.Data)
	if err != nil {
		return err
	}
	if sub.Metadata.TenantID == "" {
		return fmt.Errorf("stripe subscription %s missing metadata.tenant_id", sub.ID)
	}
	existing, err := d.svc.repo.GetSubscriptionByStripeID(ctx, sub.Metadata.TenantID, sub.ID)
	if err != nil {
		if errors.Is(err, ErrSubscriptionNotFound) {
			return nil
		}
		return err
	}
	_, err = d.svc.Cancel(ctx, existing.TenantID, existing.ID)
	return err
}

func (d *Dispatcher) handleInvoicePaid(ctx context.Context, env stripeEventEnvelope) error {
	inv, err := decodeStripeInvoiceFromEvent(env.Data)
	if err != nil {
		return err
	}
	inv.Status = InvoicePaid
	if _, err := d.svc.UpsertInvoice(ctx, inv, "invoice.paid"); err != nil {
		return err
	}
	if inv.SubscriptionID == "" {
		return nil
	}
	if existing, lookupErr := d.svc.repo.GetSubscriptionByStripeID(ctx, inv.TenantID, inv.SubscriptionID); lookupErr == nil {
		if existing.State == StatePastDue {
			_, _ = d.svc.transition(ctx, existing.TenantID, existing.ID, TransitionRecover, "subscription.updated")
		}
	}
	return nil
}

func (d *Dispatcher) handleInvoiceFailed(ctx context.Context, env stripeEventEnvelope) error {
	inv, err := decodeStripeInvoiceFromEvent(env.Data)
	if err != nil {
		return err
	}
	inv.Status = InvoiceUncollectible
	if _, err := d.svc.UpsertInvoice(ctx, inv, "invoice.failed"); err != nil {
		return err
	}
	if inv.SubscriptionID == "" {
		return nil
	}
	if existing, lookupErr := d.svc.repo.GetSubscriptionByStripeID(ctx, inv.TenantID, inv.SubscriptionID); lookupErr == nil {
		if existing.State == StateActive || existing.State == StateTrialing {
			_, _ = d.svc.transition(ctx, existing.TenantID, existing.ID, TransitionMarkPastDue, "subscription.updated")
		}
	}
	return nil
}

// mapStripeStatus translates Stripe subscription status strings to our
// canonical State. Unknown statuses default to active so we never drop
// updates silently.
func mapStripeStatus(status string) State {
	switch status {
	case "trialing":
		return StateTrialing
	case "active":
		return StateActive
	case "past_due", "unpaid":
		return StatePastDue
	case "paused":
		return StatePaused
	case "canceled", "incomplete_expired":
		return StateCanceled
	default:
		return StateActive
	}
}

func decodeStripeSubscription(data json.RawMessage) (stripeSubscriptionObject, error) {
	var wrapper stripeEventData
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return stripeSubscriptionObject{}, fmt.Errorf("decode stripe data: %w", err)
	}
	var sub stripeSubscriptionObject
	if err := json.Unmarshal(wrapper.Object, &sub); err != nil {
		return stripeSubscriptionObject{}, fmt.Errorf("decode stripe subscription: %w", err)
	}
	if sub.ID == "" {
		return stripeSubscriptionObject{}, fmt.Errorf("stripe subscription missing id")
	}
	return sub, nil
}

func decodeStripeInvoiceFromEvent(data json.RawMessage) (Invoice, error) {
	var wrapper stripeEventData
	if err := json.Unmarshal(data, &wrapper); err != nil {
		return Invoice{}, fmt.Errorf("decode stripe data: %w", err)
	}
	var inv stripeInvoiceObject
	if err := json.Unmarshal(wrapper.Object, &inv); err != nil {
		return Invoice{}, fmt.Errorf("decode stripe invoice: %w", err)
	}
	if inv.ID == "" {
		return Invoice{}, fmt.Errorf("stripe invoice missing id")
	}
	return Invoice{
		ID:             inv.ID,
		TenantID:       inv.Metadata.TenantID,
		SubscriptionID: inv.Subscription,
		Amount:         inv.AmountDue,
		Currency:       inv.Currency,
		Status:         InvoiceOpen,
		PeriodStart:    time.Unix(inv.PeriodStart, 0).UTC(),
		PeriodEnd:      time.Unix(inv.PeriodEnd, 0).UTC(),
	}, nil
}
