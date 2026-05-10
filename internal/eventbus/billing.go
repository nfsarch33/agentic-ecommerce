package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// BillingPayloadVersion is the schema version of BillingPayload. Bump
// only on a breaking change to the field set; subscribers gate on
// this value before consuming the payload.
const BillingPayloadVersion = 1

// BillingPayload is the typed envelope shipped inside Event.Payload
// for every billing.*/subscription.*/invoice.* event. TenantID is
// also at Event.TenantID; it is duplicated here so downstream
// consumers reading only the payload still get tenant scoping.
type BillingPayload struct {
	Version              int    `json:"version"`
	TenantID             string `json:"tenant_id"`
	SubscriptionID       string `json:"subscription_id"`
	PlanID               string `json:"plan_id,omitempty"`
	StripeSubscriptionID string `json:"stripe_subscription_id,omitempty"`
	State                string `json:"state,omitempty"`
	InvoiceID            string `json:"invoice_id,omitempty"`
	Amount               int    `json:"amount,omitempty"`
	Currency             string `json:"currency,omitempty"`
}

// ErrBillingPayloadInvalid is the sentinel returned by
// BillingPayload.Validate when required fields are absent.
var ErrBillingPayloadInvalid = errors.New("invalid billing payload")

// Validate returns an error when required fields are missing.
func (p BillingPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrBillingPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrBillingPayloadInvalid)
	}
	if p.SubscriptionID == "" && p.InvoiceID == "" {
		return fmt.Errorf("%w: at least one of subscription_id or invoice_id required", ErrBillingPayloadInvalid)
	}
	return nil
}

// IsBillingEvent reports whether the EventType belongs to the billing
// bounded context.
func IsBillingEvent(t EventType) bool {
	switch t {
	case SubscriptionCreated, SubscriptionUpdated, SubscriptionCanceled,
		InvoicePaid, InvoiceFailed:
		return true
	default:
		return false
	}
}

// asMap renders the typed payload as the generic map[string]any the
// in-memory bus stores in Event.Payload.
func (p BillingPayload) asMap() map[string]any {
	return map[string]any{
		"version":                p.Version,
		"tenant_id":              p.TenantID,
		"subscription_id":        p.SubscriptionID,
		"plan_id":                p.PlanID,
		"stripe_subscription_id": p.StripeSubscriptionID,
		"state":                  p.State,
		"invoice_id":             p.InvoiceID,
		"amount":                 p.Amount,
		"currency":               p.Currency,
	}
}

// NewBillingEvent is the canonical constructor every billing publisher
// path goes through. Defaults Version when zero, stamps timestamp when
// missing, and validates the payload.
//
// Source is a free-form attribution string ("mc-api.billing", "stripe",
// ...). When empty, "mc-api.billing" is used so audit trails always
// show a real producer.
func NewBillingEvent(eventType EventType, source string, occurredAt time.Time, payload BillingPayload) (Event, error) {
	if !IsBillingEvent(eventType) {
		return Event{}, fmt.Errorf("%w: %s is not a billing event", ErrBillingPayloadInvalid, eventType)
	}
	if payload.Version == 0 {
		payload.Version = BillingPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "mc-api.billing"
	}
	return Event{
		Type:      eventType,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
