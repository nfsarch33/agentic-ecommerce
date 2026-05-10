// Pricing domain payloads: supplier cost changes, price change
// approvals, and competitor price observations.
//
// Consolidated from v350_payloads.go (SupplierCost, PriceChange) and
// v390_payloads.go (CompetitorPrice) in v5.4.0.
package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// --- Supplier cost change (v3.5.0 EC-6-1) ---

const SupplierCostChangedPayloadVersion = 1

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

var ErrSupplierCostChangedInvalid = errors.New("invalid supplier cost changed payload")

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

// --- Price change approval / applied (v3.5.0 EC-6-3) ---

const PriceChangePayloadVersion = 1

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

var ErrPriceChangePayloadInvalid = errors.New("invalid price change payload")

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

func NewPriceChangePendingApprovalEvent(source string, occurredAt time.Time, payload PriceChangeApprovalPayload) (Event, error) {
	return newPriceChangeEvent(PriceChangePendingApproval, source, occurredAt, payload, "agent.pricing")
}

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

// --- Competitor price (v3.9.0 EC-6-4) ---

const CompetitorPricePayloadVersion = 1

type CompetitorPricePayload struct {
	Version          int       `json:"version"`
	TenantID         string    `json:"tenant_id"`
	SKU              string    `json:"sku"`
	Channel          string    `json:"channel"`
	CompetitorID     string    `json:"competitor_id"`
	CompetitorName   string    `json:"competitor_name,omitempty"`
	CompetitorURL    string    `json:"competitor_url,omitempty"`
	PriceAUDCents    int       `json:"price_aud_cents"`
	OurPriceAUDCents int       `json:"our_price_aud_cents,omitempty"`
	UndercutPct      float64   `json:"undercut_pct,omitempty"`
	ImageFingerprint string    `json:"image_fingerprint,omitempty"`
	ObservedAt       time.Time `json:"observed_at"`
}

var ErrCompetitorPricePayloadInvalid = errors.New("invalid competitor price payload")

func (p CompetitorPricePayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version zero", ErrCompetitorPricePayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.SKU == "" {
		return fmt.Errorf("%w: sku missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.CompetitorID == "" {
		return fmt.Errorf("%w: competitor_id missing", ErrCompetitorPricePayloadInvalid)
	}
	if p.PriceAUDCents < 0 {
		return fmt.Errorf("%w: price_aud_cents cannot be negative", ErrCompetitorPricePayloadInvalid)
	}
	return nil
}

func (p CompetitorPricePayload) asMap() map[string]any {
	return map[string]any{
		"version":             p.Version,
		"tenant_id":           p.TenantID,
		"sku":                 p.SKU,
		"channel":             p.Channel,
		"competitor_id":       p.CompetitorID,
		"competitor_name":     p.CompetitorName,
		"competitor_url":      p.CompetitorURL,
		"price_aud_cents":     p.PriceAUDCents,
		"our_price_aud_cents": p.OurPriceAUDCents,
		"undercut_pct":        p.UndercutPct,
		"image_fingerprint":   p.ImageFingerprint,
		"observed_at":         p.ObservedAt.UTC().Format(time.RFC3339Nano),
	}
}

func NewCompetitorPriceObservedEvent(source string, occurredAt time.Time, payload CompetitorPricePayload) (Event, error) {
	return newCompetitorPriceEvent(CompetitorPriceObserved, source, occurredAt, payload)
}

func NewCompetitorUndercutEvent(source string, occurredAt time.Time, payload CompetitorPricePayload) (Event, error) {
	return newCompetitorPriceEvent(CompetitorUndercut, source, occurredAt, payload)
}

func newCompetitorPriceEvent(kind EventType, source string, occurredAt time.Time, payload CompetitorPricePayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = CompetitorPricePayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.pricing.competitor_scraper"
	}
	return Event{
		Type:      kind,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
