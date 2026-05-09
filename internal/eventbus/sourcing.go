package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// SourcingProposalPayloadVersion is the schema version of
// SourcingProposalPayload. Bump on breaking change.
const SourcingProposalPayloadVersion = 1

// SourcingProposalPayload is the v3.1.0 EC-1-3 typed envelope shipped
// inside Event.Payload for every product.sourcing.proposed event.
// TenantID also lives at Event.TenantID; the duplication mirrors the
// MembershipPayload pattern so consumers reading only the payload
// still have tenant scoping.
type SourcingProposalPayload struct {
	Version          int                       `json:"version"`
	TenantID         string                    `json:"tenant_id"`
	Keyword          string                    `json:"keyword"`
	Source           string                    `json:"source"`
	GeneratedAt      time.Time                 `json:"generated_at"`
	SelectedProducts []SourcingProposalProduct `json:"selected_products"`
	RejectedCount    int                       `json:"rejected_count"`
	RejectedReasons  map[string]int            `json:"rejected_reasons,omitempty"`
	SupplierScore    float64                   `json:"supplier_score"`
	MarginScore      float64                   `json:"margin_score"`
	TrendScore       float64                   `json:"trend_score"`
	CompositeScore   float64                   `json:"composite_score"`
}

// SourcingProposalProduct is the per-product slice of the proposal.
// Kept slim so consumers (Grafana, dashboards) can render directly
// off the JSON shape.
type SourcingProposalProduct struct {
	ExternalID    string  `json:"external_id"`
	Source        string  `json:"source"`
	Title         string  `json:"title"`
	Category      string  `json:"category"`
	PriceCNYCents int     `json:"price_cny_cents"`
	MOQ           int     `json:"moq"`
	LeadTimeDays  int     `json:"lead_time_days"`
	SupplierID    string  `json:"supplier_id"`
	SupplierScore float64 `json:"supplier_score"`
	URL           string  `json:"url"`
}

// ErrSourcingPayloadInvalid is the sentinel returned by
// SourcingProposalPayload.Validate when required fields are missing.
var ErrSourcingPayloadInvalid = errors.New("invalid sourcing proposal payload")

// Validate enforces required identity + integrity fields. Wraps
// ErrSourcingPayloadInvalid so callers can errors.Is.
func (p SourcingProposalPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrSourcingPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrSourcingPayloadInvalid)
	}
	if p.Keyword == "" {
		return fmt.Errorf("%w: keyword missing", ErrSourcingPayloadInvalid)
	}
	if len(p.SelectedProducts) == 0 {
		return fmt.Errorf("%w: at least one selected product required", ErrSourcingPayloadInvalid)
	}
	for i, sp := range p.SelectedProducts {
		if sp.ExternalID == "" {
			return fmt.Errorf("%w: selected_products[%d].external_id missing", ErrSourcingPayloadInvalid, i)
		}
	}
	return nil
}

// asMap renders the typed payload as the generic map[string]any the
// in-memory bus stores in Event.Payload. Keep field names in sync
// with json tags so downstream Streams/JSON consumers see one canonical
// shape.
func (p SourcingProposalPayload) asMap() map[string]any {
	products := make([]any, 0, len(p.SelectedProducts))
	for _, sp := range p.SelectedProducts {
		products = append(products, map[string]any{
			"external_id":     sp.ExternalID,
			"source":          sp.Source,
			"title":           sp.Title,
			"category":        sp.Category,
			"price_cny_cents": sp.PriceCNYCents,
			"moq":             sp.MOQ,
			"lead_time_days":  sp.LeadTimeDays,
			"supplier_id":     sp.SupplierID,
			"supplier_score":  sp.SupplierScore,
			"url":             sp.URL,
		})
	}
	rejected := make(map[string]any, len(p.RejectedReasons))
	for k, v := range p.RejectedReasons {
		rejected[k] = v
	}
	return map[string]any{
		"version":           p.Version,
		"tenant_id":         p.TenantID,
		"keyword":           p.Keyword,
		"source":            p.Source,
		"generated_at":      p.GeneratedAt.UTC().Format(time.RFC3339Nano),
		"selected_products": products,
		"rejected_count":    p.RejectedCount,
		"rejected_reasons":  rejected,
		"supplier_score":    p.SupplierScore,
		"margin_score":      p.MarginScore,
		"trend_score":       p.TrendScore,
		"composite_score":   p.CompositeScore,
	}
}

// NewSourcingProposalEvent is the canonical constructor every
// sourcing agent path goes through. Defaults Version when zero,
// stamps the timestamp when missing, and validates the payload.
func NewSourcingProposalEvent(source string, occurredAt time.Time, payload SourcingProposalPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = SourcingProposalPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.sourcing.china"
	}
	return Event{
		Type:      ProductSourcingProposed,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
