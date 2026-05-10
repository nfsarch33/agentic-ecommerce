// File scope: v3.8.0 typed event payloads for Epic 7 (logistics +
// returns) + Epic 9 (ROI). All payloads follow the v3.5.0 envelope
// pattern: typed Validate, typed asMap, typed constructor.
//
// Reuse evidence:
//   - Pattern mirrors v3.5.0 EC-6/EC-7 (v350_payloads.go).
//   - Error sentinel + %w-wrap from the package convention.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// ShipmentLabelGeneratedPayloadVersion is the schema version.
const ShipmentLabelGeneratedPayloadVersion = 1

// ShipmentLabelGeneratedPayload is the v3.8.0 EC-7-3 envelope.
// Emitted by the shipping label generator after a carrier-issued
// label is produced.
type ShipmentLabelGeneratedPayload struct {
	Version        int       `json:"version"`
	TenantID       string    `json:"tenant_id"`
	OrderID        string    `json:"order_id"`
	Carrier        string    `json:"carrier"`
	TrackingNumber string    `json:"tracking_number"`
	LabelPDFURL    string    `json:"label_pdf_url"`
	CostAUDCents   int       `json:"cost_aud_cents"`
	ETADays        int       `json:"eta_days"`
	SLADays        int       `json:"sla_days"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// ErrShipmentLabelGeneratedInvalid is returned by Validate.
var ErrShipmentLabelGeneratedInvalid = errors.New("invalid shipment label generated payload")

// Validate enforces required fields.
func (p ShipmentLabelGeneratedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrShipmentLabelGeneratedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrShipmentLabelGeneratedInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrShipmentLabelGeneratedInvalid)
	}
	if p.Carrier == "" {
		return fmt.Errorf("%w: carrier missing", ErrShipmentLabelGeneratedInvalid)
	}
	if p.TrackingNumber == "" {
		return fmt.Errorf("%w: tracking_number missing", ErrShipmentLabelGeneratedInvalid)
	}
	return nil
}

func (p ShipmentLabelGeneratedPayload) asMap() map[string]any {
	return map[string]any{
		"version":         p.Version,
		"tenant_id":       p.TenantID,
		"order_id":        p.OrderID,
		"carrier":         p.Carrier,
		"tracking_number": p.TrackingNumber,
		"label_pdf_url":   p.LabelPDFURL,
		"cost_aud_cents":  p.CostAUDCents,
		"eta_days":        p.ETADays,
		"sla_days":        p.SLADays,
		"occurred_at":     p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewShipmentLabelGeneratedEvent is the canonical constructor.
func NewShipmentLabelGeneratedEvent(source string, occurredAt time.Time, payload ShipmentLabelGeneratedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ShipmentLabelGeneratedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.fulfilment.shipping_label"
	}
	return Event{
		Type:      ShipmentLabelGenerated,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// ShipmentStatusUpdatedPayloadVersion is the schema version.
const ShipmentStatusUpdatedPayloadVersion = 1

// ShipmentStatusUpdatedPayload is the v3.8.0 EC-7-4 envelope.
// Emitted by the carrier webhook handlers when shipment status
// transitions.
type ShipmentStatusUpdatedPayload struct {
	Version        int       `json:"version"`
	TenantID       string    `json:"tenant_id"`
	OrderID        string    `json:"order_id"`
	Carrier        string    `json:"carrier"`
	TrackingNumber string    `json:"tracking_number"`
	Status         string    `json:"status"` // shipped|in_transit|delivered|exception
	EventID        string    `json:"event_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

// ErrShipmentStatusUpdatedInvalid is returned by Validate.
var ErrShipmentStatusUpdatedInvalid = errors.New("invalid shipment status updated payload")

// Validate enforces required fields.
func (p ShipmentStatusUpdatedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrShipmentStatusUpdatedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrShipmentStatusUpdatedInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrShipmentStatusUpdatedInvalid)
	}
	if p.TrackingNumber == "" {
		return fmt.Errorf("%w: tracking_number missing", ErrShipmentStatusUpdatedInvalid)
	}
	if p.Status == "" {
		return fmt.Errorf("%w: status missing", ErrShipmentStatusUpdatedInvalid)
	}
	if p.EventID == "" {
		return fmt.Errorf("%w: event_id missing (idempotency key)", ErrShipmentStatusUpdatedInvalid)
	}
	return nil
}

func (p ShipmentStatusUpdatedPayload) asMap() map[string]any {
	return map[string]any{
		"version":         p.Version,
		"tenant_id":       p.TenantID,
		"order_id":        p.OrderID,
		"carrier":         p.Carrier,
		"tracking_number": p.TrackingNumber,
		"status":          p.Status,
		"event_id":        p.EventID,
		"occurred_at":     p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewShipmentStatusUpdatedEvent is the canonical constructor.
func NewShipmentStatusUpdatedEvent(source string, occurredAt time.Time, payload ShipmentStatusUpdatedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ShipmentStatusUpdatedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "webhook.carrier"
	}
	kind := ShipmentStatusUpdated
	if payload.Status == "delivered" {
		kind = OrderDelivered
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// ReturnsSagaPayloadVersion is the schema version shared by the
// returns lifecycle events.
const ReturnsSagaPayloadVersion = 1

// ReturnsSagaPayload is the v3.8.0 EC-7-5 envelope. Used by every
// returns-saga lifecycle event so the dashboard can pivot on a
// single shape.
type ReturnsSagaPayload struct {
	Version           int       `json:"version"`
	TenantID          string    `json:"tenant_id"`
	RMAID             string    `json:"rma_id"`
	OrderID           string    `json:"order_id"`
	Reason            string    `json:"reason"`
	RefundAmountCents int       `json:"refund_amount_aud_cents"`
	AutoApproved      bool      `json:"auto_approved"`
	State             string    `json:"state"` // requested|approved|labelled|refunded|completed|rolled_back
	RolledBackReason  string    `json:"rolled_back_reason,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
}

// ErrReturnsSagaPayloadInvalid is returned by Validate.
var ErrReturnsSagaPayloadInvalid = errors.New("invalid returns saga payload")

// Validate enforces required fields.
func (p ReturnsSagaPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrReturnsSagaPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrReturnsSagaPayloadInvalid)
	}
	if p.RMAID == "" {
		return fmt.Errorf("%w: rma_id missing", ErrReturnsSagaPayloadInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrReturnsSagaPayloadInvalid)
	}
	if p.RefundAmountCents < 0 {
		return fmt.Errorf("%w: refund_amount cannot be negative", ErrReturnsSagaPayloadInvalid)
	}
	if p.State == "" {
		return fmt.Errorf("%w: state missing", ErrReturnsSagaPayloadInvalid)
	}
	return nil
}

func (p ReturnsSagaPayload) asMap() map[string]any {
	return map[string]any{
		"version":                 p.Version,
		"tenant_id":               p.TenantID,
		"rma_id":                  p.RMAID,
		"order_id":                p.OrderID,
		"reason":                  p.Reason,
		"refund_amount_aud_cents": p.RefundAmountCents,
		"auto_approved":           p.AutoApproved,
		"state":                   p.State,
		"rolled_back_reason":      p.RolledBackReason,
		"occurred_at":             p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewReturnRequestedEvent fires when a customer initiates a return.
func NewReturnRequestedEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(ReturnRequested, source, occurredAt, payload)
}

// NewLargeRefundPendingApprovalEvent fires when refund_amount >= the
// auto-approval threshold (default A$50 = 5000 cents).
func NewLargeRefundPendingApprovalEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(LargeRefundPendingApproval, source, occurredAt, payload)
}

// NewReturnsSagaCompletedEvent fires after the full refund flow ran
// to completion (label issued + refund processed + channel updated).
func NewReturnsSagaCompletedEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(ReturnsSagaCompleted, source, occurredAt, payload)
}

// NewReturnsSagaRolledBackEvent fires when any saga step failed and
// the compensating activities ran.
func NewReturnsSagaRolledBackEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(ReturnsSagaRolledBack, source, occurredAt, payload)
}

func newReturnsSagaEvent(kind EventType, source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ReturnsSagaPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "workflow.returns_saga"
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
