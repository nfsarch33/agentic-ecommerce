// Fulfilment domain payloads: order normalisation, dropship lifecycle,
// shipping labels/status, and returns saga.
//
// Consolidated from v350_payloads.go (OrderNormalised, Dropship) and
// v380_payloads.go (ShipmentLabel, ShipmentStatus, ReturnsSaga) in v5.4.0.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// --- Order normalised (v3.5.0 EC-7-1) ---

const OrderNormalisedPayloadVersion = 1

type OrderNormalisedLine struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int    `json:"unit_cents"`
	ProductID string `json:"product_id,omitempty"`
}

type OrderNormalisedPayload struct {
	Version         int                   `json:"version"`
	TenantID        string                `json:"tenant_id"`
	OrderID         string                `json:"order_id"`
	ExternalOrderID string                `json:"external_order_id"`
	Channel         string                `json:"channel"`
	BuyerEmail      string                `json:"buyer_email,omitempty"`
	TotalAUDCents   int                   `json:"total_aud_cents"`
	Currency        string                `json:"currency"`
	Items           []OrderNormalisedLine `json:"items"`
	Status          string                `json:"status,omitempty"`
	ShippingCountry string                `json:"shipping_country,omitempty"`
	OccurredAt      time.Time             `json:"occurred_at"`
}

var ErrOrderNormalisedInvalid = errors.New("invalid order normalised payload")

func (p OrderNormalisedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrOrderNormalisedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrOrderNormalisedInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrOrderNormalisedInvalid)
	}
	if p.ExternalOrderID == "" {
		return fmt.Errorf("%w: external_order_id missing", ErrOrderNormalisedInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrOrderNormalisedInvalid)
	}
	if len(p.Items) == 0 {
		return fmt.Errorf("%w: at least one item required", ErrOrderNormalisedInvalid)
	}
	return nil
}

func (p OrderNormalisedPayload) asMap() map[string]any {
	items := make([]any, 0, len(p.Items))
	for _, line := range p.Items {
		items = append(items, map[string]any{
			"sku":        line.SKU,
			"quantity":   line.Quantity,
			"unit_cents": line.UnitCents,
			"product_id": line.ProductID,
		})
	}
	return map[string]any{
		"version":           p.Version,
		"tenant_id":         p.TenantID,
		"order_id":          p.OrderID,
		"external_order_id": p.ExternalOrderID,
		"channel":           p.Channel,
		"buyer_email":       p.BuyerEmail,
		"total_aud_cents":   p.TotalAUDCents,
		"currency":          p.Currency,
		"items":             items,
		"status":            p.Status,
		"shipping_country":  p.ShippingCountry,
		"occurred_at":       p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

func NewOrderNormalisedEvent(source string, occurredAt time.Time, payload OrderNormalisedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = OrderNormalisedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "workflow.order_aggregator"
	}
	return Event{
		Type:      OrderNormalised,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// --- Dropship order lifecycle (v3.5.0 EC-7-2) ---

const DropshipOrderPayloadVersion = 1

type DropshipOrderPayload struct {
	Version         int       `json:"version"`
	TenantID        string    `json:"tenant_id"`
	OrderID         string    `json:"order_id"`
	Supplier        string    `json:"supplier"`
	SupplierOrderID string    `json:"supplier_order_id,omitempty"`
	TotalAUDCents   int       `json:"total_aud_cents"`
	Reason          string    `json:"reason,omitempty"`
	OccurredAt      time.Time `json:"occurred_at"`
}

var ErrDropshipOrderPayloadInvalid = errors.New("invalid dropship order payload")

func (p DropshipOrderPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrDropshipOrderPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrDropshipOrderPayloadInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrDropshipOrderPayloadInvalid)
	}
	if p.Supplier == "" {
		return fmt.Errorf("%w: supplier missing", ErrDropshipOrderPayloadInvalid)
	}
	if p.TotalAUDCents < 0 {
		return fmt.Errorf("%w: total_aud_cents cannot be negative", ErrDropshipOrderPayloadInvalid)
	}
	return nil
}

func (p DropshipOrderPayload) asMap() map[string]any {
	return map[string]any{
		"version":           p.Version,
		"tenant_id":         p.TenantID,
		"order_id":          p.OrderID,
		"supplier":          p.Supplier,
		"supplier_order_id": p.SupplierOrderID,
		"total_aud_cents":   p.TotalAUDCents,
		"reason":            p.Reason,
		"occurred_at":       p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

func NewLargeDropshipOrderPendingApprovalEvent(source string, occurredAt time.Time, payload DropshipOrderPayload) (Event, error) {
	return newDropshipEvent(LargeDropshipOrderPendingApproval, source, occurredAt, payload)
}

func NewDropshipOrderPlacedEvent(source string, occurredAt time.Time, payload DropshipOrderPayload) (Event, error) {
	return newDropshipEvent(DropshipOrderPlaced, source, occurredAt, payload)
}

func NewDropshipOrderRolledBackEvent(source string, occurredAt time.Time, payload DropshipOrderPayload) (Event, error) {
	return newDropshipEvent(DropshipOrderRolledBack, source, occurredAt, payload)
}

func newDropshipEvent(kind EventType, source string, occurredAt time.Time, payload DropshipOrderPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = DropshipOrderPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.fulfilment.dropship"
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// --- Shipment label generated (v3.8.0 EC-7-3) ---

const ShipmentLabelGeneratedPayloadVersion = 1

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

var ErrShipmentLabelGeneratedInvalid = errors.New("invalid shipment label generated payload")

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

// --- Shipment status updated (v3.8.0 EC-7-4) ---

const ShipmentStatusUpdatedPayloadVersion = 1

type ShipmentStatusUpdatedPayload struct {
	Version        int       `json:"version"`
	TenantID       string    `json:"tenant_id"`
	OrderID        string    `json:"order_id"`
	Carrier        string    `json:"carrier"`
	TrackingNumber string    `json:"tracking_number"`
	Status         string    `json:"status"`
	EventID        string    `json:"event_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

var ErrShipmentStatusUpdatedInvalid = errors.New("invalid shipment status updated payload")

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

// --- Returns saga (v3.8.0 EC-7-5) ---

const ReturnsSagaPayloadVersion = 1

type ReturnsSagaPayload struct {
	Version           int       `json:"version"`
	TenantID          string    `json:"tenant_id"`
	RMAID             string    `json:"rma_id"`
	OrderID           string    `json:"order_id"`
	Reason            string    `json:"reason"`
	RefundAmountCents int       `json:"refund_amount_aud_cents"`
	AutoApproved      bool      `json:"auto_approved"`
	State             string    `json:"state"`
	RolledBackReason  string    `json:"rolled_back_reason,omitempty"`
	OccurredAt        time.Time `json:"occurred_at"`
}

var ErrReturnsSagaPayloadInvalid = errors.New("invalid returns saga payload")

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

func NewReturnRequestedEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(ReturnRequested, source, occurredAt, payload)
}

func NewLargeRefundPendingApprovalEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(LargeRefundPendingApproval, source, occurredAt, payload)
}

func NewReturnsSagaCompletedEvent(source string, occurredAt time.Time, payload ReturnsSagaPayload) (Event, error) {
	return newReturnsSagaEvent(ReturnsSagaCompleted, source, occurredAt, payload)
}

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
