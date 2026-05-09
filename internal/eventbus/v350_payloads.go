// File scope: v3.5.0 typed event payloads for Epic 6 (pricing) +
// Epic 7 (fulfilment seed). All five payloads follow the v3.3.0 +
// v3.4.0 envelope pattern: typed Validate, typed asMap, typed
// constructor.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// SupplierCostChangedPayloadVersion is the schema version of
// SupplierCostChangedPayload. Bump on breaking change.
const SupplierCostChangedPayloadVersion = 1

// SupplierCostChangedPayload is the v3.5.0 EC-6-1 envelope. Emitted
// by the supplier cost monitor when the observed CNY-denominated
// supplier cost drifts outside the configured threshold (default 5%)
// from the stored baseline.
type SupplierCostChangedPayload struct {
	Version          int       `json:"version"`
	TenantID         string    `json:"tenant_id"`
	Source           string    `json:"source"`
	SupplierSKU      string    `json:"supplier_sku"`
	SupplierID       string    `json:"supplier_id,omitempty"`
	BaselineCNYCents int       `json:"baseline_cny_cents"`
	ObservedCNYCents int       `json:"observed_cny_cents"`
	DeltaPct         float64   `json:"delta_pct"`
	Direction        string    `json:"direction"`
	ThresholdPct     float64   `json:"threshold_pct"`
	ObservedAt       time.Time `json:"observed_at"`
}

// ErrSupplierCostChangedInvalid is returned by Validate.
var ErrSupplierCostChangedInvalid = errors.New("invalid supplier cost changed payload")

// Validate enforces required identity + integrity fields.
func (p SupplierCostChangedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrSupplierCostChangedInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrSupplierCostChangedInvalid)
	}
	if p.Source == "" {
		return fmt.Errorf("%w: source missing", ErrSupplierCostChangedInvalid)
	}
	if p.SupplierSKU == "" {
		return fmt.Errorf("%w: supplier_sku missing", ErrSupplierCostChangedInvalid)
	}
	if p.BaselineCNYCents < 0 || p.ObservedCNYCents < 0 {
		return fmt.Errorf("%w: cost cannot be negative", ErrSupplierCostChangedInvalid)
	}
	if p.Direction != "up" && p.Direction != "down" {
		return fmt.Errorf("%w: direction must be up|down (got %q)", ErrSupplierCostChangedInvalid, p.Direction)
	}
	return nil
}

func (p SupplierCostChangedPayload) asMap() map[string]any {
	return map[string]any{
		"version":            p.Version,
		"tenant_id":          p.TenantID,
		"source":             p.Source,
		"supplier_sku":       p.SupplierSKU,
		"supplier_id":        p.SupplierID,
		"baseline_cny_cents": p.BaselineCNYCents,
		"observed_cny_cents": p.ObservedCNYCents,
		"delta_pct":          p.DeltaPct,
		"direction":          p.Direction,
		"threshold_pct":      p.ThresholdPct,
		"observed_at":        p.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewSupplierCostChangedEvent is the canonical constructor.
func NewSupplierCostChangedEvent(source string, occurredAt time.Time, payload SupplierCostChangedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = SupplierCostChangedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "monitor.supplier_cost"
	}
	return Event{
		Type:      SupplierCostChanged,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// PriceChangePayloadVersion is the schema version of
// PriceChangeApprovalPayload + PriceChangeAppliedPayload (single
// version constant covers both since they share the shape).
const PriceChangePayloadVersion = 1

// PriceChangeApprovalPayload is the v3.5.0 EC-6-3 envelope used by
// both the pending-approval gate and the applied event so dashboards
// can pivot on a single shape.
type PriceChangeApprovalPayload struct {
	Version               int       `json:"version"`
	TenantID              string    `json:"tenant_id"`
	ProductID             string    `json:"product_id"`
	Channel               string    `json:"channel"`
	OldPriceAUDCents      int       `json:"old_price_aud_cents"`
	ProposedPriceAUDCents int       `json:"proposed_price_aud_cents"`
	DeltaPct              float64   `json:"delta_pct"`
	Reason                string    `json:"reason"`
	DecisionSource        string    `json:"decision_source"`
	GuardrailFloorPct     float64   `json:"guardrail_floor_pct,omitempty"`
	OccurredAt            time.Time `json:"occurred_at"`
}

// ErrPriceChangePayloadInvalid is the sentinel for missing fields.
var ErrPriceChangePayloadInvalid = errors.New("invalid price change payload")

// Validate enforces required identity + integrity fields.
func (p PriceChangeApprovalPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrPriceChangePayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrPriceChangePayloadInvalid)
	}
	if p.ProductID == "" {
		return fmt.Errorf("%w: product_id missing", ErrPriceChangePayloadInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrPriceChangePayloadInvalid)
	}
	if p.ProposedPriceAUDCents <= 0 {
		return fmt.Errorf("%w: proposed_price_aud_cents must be > 0", ErrPriceChangePayloadInvalid)
	}
	if p.OldPriceAUDCents < 0 {
		return fmt.Errorf("%w: old_price_aud_cents cannot be negative", ErrPriceChangePayloadInvalid)
	}
	return nil
}

func (p PriceChangeApprovalPayload) asMap() map[string]any {
	return map[string]any{
		"version":                  p.Version,
		"tenant_id":                p.TenantID,
		"product_id":               p.ProductID,
		"channel":                  p.Channel,
		"old_price_aud_cents":      p.OldPriceAUDCents,
		"proposed_price_aud_cents": p.ProposedPriceAUDCents,
		"delta_pct":                p.DeltaPct,
		"reason":                   p.Reason,
		"decision_source":          p.DecisionSource,
		"guardrail_floor_pct":      p.GuardrailFloorPct,
		"occurred_at":              p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewPriceChangePendingApprovalEvent emits the gate event for
// proposed deltas above the operator-configured threshold.
func NewPriceChangePendingApprovalEvent(source string, occurredAt time.Time, payload PriceChangeApprovalPayload) (Event, error) {
	return newPriceChangeEvent(PriceChangePendingApproval, source, occurredAt, payload, "agent.pricing")
}

// NewPriceChangeAppliedEvent emits the applied event for changes
// below the threshold (auto-applied) or after operator approval.
func NewPriceChangeAppliedEvent(source string, occurredAt time.Time, payload PriceChangeApprovalPayload) (Event, error) {
	return newPriceChangeEvent(PriceChangeApplied, source, occurredAt, payload, "agent.pricing")
}

func newPriceChangeEvent(kind EventType, source string, occurredAt time.Time, payload PriceChangeApprovalPayload, defaultSource string) (Event, error) {
	if payload.Version == 0 {
		payload.Version = PriceChangePayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = defaultSource
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// OrderNormalisedPayloadVersion is the schema version of
// OrderNormalisedPayload.
const OrderNormalisedPayloadVersion = 1

// OrderNormalisedLine mirrors OrderReceivedLine but on the
// post-normalisation domain side. Tenant + channel context lives
// on the parent payload.
type OrderNormalisedLine struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int    `json:"unit_cents"`
	ProductID string `json:"product_id,omitempty"`
}

// OrderNormalisedPayload is the v3.5.0 EC-7-1 envelope. Emitted by
// the multi-channel order aggregator after dedup + normalisation.
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

// ErrOrderNormalisedInvalid is the sentinel for missing fields.
var ErrOrderNormalisedInvalid = errors.New("invalid order normalised payload")

// Validate enforces required fields.
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

// NewOrderNormalisedEvent is the canonical constructor.
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

// DropshipOrderPayloadVersion is the schema version of the
// DropshipOrderPayload (shared by the placed + pending-approval +
// rolled-back events).
const DropshipOrderPayloadVersion = 1

// DropshipOrderPayload is the v3.5.0 EC-7-2 envelope used by the
// drop-ship lifecycle events.
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

// ErrDropshipOrderPayloadInvalid is the sentinel for missing fields.
var ErrDropshipOrderPayloadInvalid = errors.New("invalid dropship order payload")

// Validate enforces required fields.
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

// NewLargeDropshipOrderPendingApprovalEvent fires when total > A$500.
func NewLargeDropshipOrderPendingApprovalEvent(source string, occurredAt time.Time, payload DropshipOrderPayload) (Event, error) {
	return newDropshipEvent(LargeDropshipOrderPendingApproval, source, occurredAt, payload)
}

// NewDropshipOrderPlacedEvent fires after the supplier adapter
// successfully placed the order (primary or fallback).
func NewDropshipOrderPlacedEvent(source string, occurredAt time.Time, payload DropshipOrderPayload) (Event, error) {
	return newDropshipEvent(DropshipOrderPlaced, source, occurredAt, payload)
}

// NewDropshipOrderRolledBackEvent fires when every supplier adapter
// failed AND the customer-side fulfillment trigger was rolled back.
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
