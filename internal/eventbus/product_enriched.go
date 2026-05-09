package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// ProductEnrichedPayloadVersion is the schema version of
// ProductEnrichedPayload. Bump on breaking change.
const ProductEnrichedPayloadVersion = 1

// ProductEnrichedPayload is the v3.2.0 + v3.3.0 typed envelope shipped
// inside Event.Payload for every product.enriched event. The EC-2
// enrichment pipeline emits these; the EC-3-2 TikTok listing agent
// (and other channel adapters in v3.4) subscribes to them.
//
// TenantID also lives at Event.TenantID; the duplication mirrors the
// MembershipPayload + SourcingProposalPayload pattern so consumers
// reading only the payload still have tenant scoping.
type ProductEnrichedPayload struct {
	Version            int      `json:"version"`
	TenantID           string   `json:"tenant_id"`
	ProductID          string   `json:"product_id"`
	ExternalID         string   `json:"external_id"`
	EnglishTitle       string   `json:"english_title"`
	EnglishDescription string   `json:"english_description"`
	CategoryID         string   `json:"category_id"`
	BrandName          string   `json:"brand_name,omitempty"`
	PriceCents         int      `json:"price_cents"`
	Currency           string   `json:"currency"`
	StockUnits         int      `json:"stock_units"`
	ShippingTemplate   string   `json:"shipping_template,omitempty"`
	Images             []string `json:"images,omitempty"`
	VideoSKUURL        string   `json:"video_sku_url,omitempty"`
	SellerSKU          string   `json:"seller_sku,omitempty"`
	WarehouseID        string   `json:"warehouse_id,omitempty"`
	QualityScore       float64  `json:"quality_score"`
	Source             string   `json:"source,omitempty"`
}

// ErrProductEnrichedPayloadInvalid is the sentinel returned by
// ProductEnrichedPayload.Validate when required fields are missing.
var ErrProductEnrichedPayloadInvalid = errors.New("invalid product enriched payload")

// Validate enforces required identity + integrity fields.
func (p ProductEnrichedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrProductEnrichedPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrProductEnrichedPayloadInvalid)
	}
	if p.ProductID == "" {
		return fmt.Errorf("%w: product_id missing", ErrProductEnrichedPayloadInvalid)
	}
	if p.EnglishTitle == "" {
		return fmt.Errorf("%w: english_title missing", ErrProductEnrichedPayloadInvalid)
	}
	if p.PriceCents <= 0 {
		return fmt.Errorf("%w: price_cents must be > 0", ErrProductEnrichedPayloadInvalid)
	}
	return nil
}

// asMap renders the typed payload as the generic map[string]any the
// in-memory bus stores in Event.Payload.
func (p ProductEnrichedPayload) asMap() map[string]any {
	images := make([]any, 0, len(p.Images))
	for _, img := range p.Images {
		images = append(images, img)
	}
	return map[string]any{
		"version":             p.Version,
		"tenant_id":           p.TenantID,
		"product_id":          p.ProductID,
		"external_id":         p.ExternalID,
		"english_title":       p.EnglishTitle,
		"english_description": p.EnglishDescription,
		"category_id":         p.CategoryID,
		"brand_name":          p.BrandName,
		"price_cents":         p.PriceCents,
		"currency":            p.Currency,
		"stock_units":         p.StockUnits,
		"shipping_template":   p.ShippingTemplate,
		"images":              images,
		"video_sku_url":       p.VideoSKUURL,
		"seller_sku":          p.SellerSKU,
		"warehouse_id":        p.WarehouseID,
		"quality_score":       p.QualityScore,
		"source":              p.Source,
	}
}

// NewProductEnrichedEvent is the canonical constructor every
// enrichment publisher path goes through. Defaults Version when zero,
// stamps the timestamp when missing, and validates the payload.
func NewProductEnrichedEvent(source string, occurredAt time.Time, payload ProductEnrichedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = ProductEnrichedPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.enrichment"
	}
	return Event{
		Type:      ProductEnriched,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}

// TikTokListingRollbackPayloadVersion is the schema version of
// TikTokListingRollbackPayload.
const TikTokListingRollbackPayloadVersion = 1

// TikTokListingRollbackPayload carries enough context for downstream
// dashboards / operator alerts to identify the failed publish.
type TikTokListingRollbackPayload struct {
	Version    int    `json:"version"`
	TenantID   string `json:"tenant_id"`
	ProductID  string `json:"product_id"`
	RemoteID   string `json:"remote_id,omitempty"`
	Reason     string `json:"reason"`
	Stage      string `json:"stage"`
	OccurredAt string `json:"occurred_at"`
}

// ErrTikTokListingRollbackInvalid is the sentinel for missing fields.
var ErrTikTokListingRollbackInvalid = errors.New("invalid tiktok listing rollback payload")

// Validate enforces required fields.
func (p TikTokListingRollbackPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrTikTokListingRollbackInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrTikTokListingRollbackInvalid)
	}
	if p.ProductID == "" {
		return fmt.Errorf("%w: product_id missing", ErrTikTokListingRollbackInvalid)
	}
	if p.Reason == "" {
		return fmt.Errorf("%w: reason missing", ErrTikTokListingRollbackInvalid)
	}
	return nil
}

func (p TikTokListingRollbackPayload) asMap() map[string]any {
	return map[string]any{
		"version":     p.Version,
		"tenant_id":   p.TenantID,
		"product_id":  p.ProductID,
		"remote_id":   p.RemoteID,
		"reason":      p.Reason,
		"stage":       p.Stage,
		"occurred_at": p.OccurredAt,
	}
}

// NewTikTokListingRollbackEvent is the canonical constructor.
func NewTikTokListingRollbackEvent(source string, occurredAt time.Time, payload TikTokListingRollbackPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = TikTokListingRollbackPayloadVersion
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if source == "" {
		source = "agent.channel.tiktok"
	}
	return Event{
		Type:      TikTokListingRolledBack,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
