package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// PaymentSagaPayloadVersion is the schema version.
const PaymentSagaPayloadVersion = 1

// PaymentSagaPayload is the v4.2.0 envelope shared by all payment
// lifecycle events. Follows the v3.5.0/v3.8.0 typed-payload pattern.
type PaymentSagaPayload struct {
	Version     int       `json:"version"`
	TenantID    string    `json:"tenant_id"`
	OrderID     string    `json:"order_id"`
	PaymentID   string    `json:"payment_id"`
	Provider    string    `json:"provider"`
	AmountCents int64     `json:"amount_cents"`
	Currency    string    `json:"currency"`
	Status      string    `json:"status"`
	FailReason  string    `json:"fail_reason,omitempty"`
	OccurredAt  time.Time `json:"occurred_at"`
}

// ErrPaymentSagaPayloadInvalid is returned by Validate.
var ErrPaymentSagaPayloadInvalid = errors.New("invalid payment saga payload")

// Validate enforces required fields.
func (p PaymentSagaPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrPaymentSagaPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrPaymentSagaPayloadInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrPaymentSagaPayloadInvalid)
	}
	if p.Provider == "" {
		return fmt.Errorf("%w: provider missing", ErrPaymentSagaPayloadInvalid)
	}
	if p.Status == "" {
		return fmt.Errorf("%w: status missing", ErrPaymentSagaPayloadInvalid)
	}
	return nil
}

func (p PaymentSagaPayload) asMap() map[string]any {
	return map[string]any{
		"version":      p.Version,
		"tenant_id":    p.TenantID,
		"order_id":     p.OrderID,
		"payment_id":   p.PaymentID,
		"provider":     p.Provider,
		"amount_cents": p.AmountCents,
		"currency":     p.Currency,
		"status":       p.Status,
		"fail_reason":  p.FailReason,
		"occurred_at":  p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewPaymentCompletedEvent fires when charge succeeds.
func NewPaymentCompletedEvent(source string, occurredAt time.Time, payload PaymentSagaPayload) (Event, error) {
	return newPaymentSagaEvent(PaymentCompleted, source, occurredAt, payload)
}

// NewPaymentFailedEvent fires on persistent charge failure.
func NewPaymentFailedEvent(source string, occurredAt time.Time, payload PaymentSagaPayload) (Event, error) {
	return newPaymentSagaEvent(PaymentFailed, source, occurredAt, payload)
}

// NewPaymentRefundRequestedEvent fires when a refund is requested
// through the payment saga.
func NewPaymentRefundRequestedEvent(source string, occurredAt time.Time, payload PaymentSagaPayload) (Event, error) {
	return newPaymentSagaEvent(PaymentRefundRequested, source, occurredAt, payload)
}

func newPaymentSagaEvent(kind EventType, source string, occurredAt time.Time, payload PaymentSagaPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = PaymentSagaPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "workflow.payment_saga"
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
