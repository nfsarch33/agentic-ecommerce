package eventbus

import (
	"errors"
	"fmt"
	"time"
)

// OrderReceivedPayloadVersion is the schema version of
// OrderReceivedPayload. Bump on breaking change.
const OrderReceivedPayloadVersion = 1

// OrderReceivedLine is one line item inside an OrderReceivedPayload.
type OrderReceivedLine struct {
	SKU         string `json:"sku"`
	Quantity    int    `json:"quantity"`
	UnitCents   int    `json:"unit_cents"`
	ProductID   string `json:"product_id,omitempty"`
	WarehouseID string `json:"warehouse_id,omitempty"`
}

// OrderReceivedPayload is the v3.3.0 EC-3-3 typed envelope shipped
// inside Event.Payload for every order.received event. Mirrors the
// social.TikTokOrder shape so the webhook layer can adapt 1:1.
type OrderReceivedPayload struct {
	Version        int                 `json:"version"`
	TenantID       string              `json:"tenant_id"`
	OrderID        string              `json:"order_id"`
	ShopID         string              `json:"shop_id"`
	Channel        string              `json:"channel"`
	BuyerEmail     string              `json:"buyer_email,omitempty"`
	TotalCents     int                 `json:"total_cents"`
	Currency       string              `json:"currency"`
	Items          []OrderReceivedLine `json:"items"`
	Status         string              `json:"status,omitempty"`
	IdempotencyKey string              `json:"idempotency_key"`
	OccurredAt     time.Time           `json:"occurred_at"`
}

// ErrOrderReceivedPayloadInvalid is the sentinel returned by Validate.
var ErrOrderReceivedPayloadInvalid = errors.New("invalid order received payload")

// Validate enforces required fields.
func (p OrderReceivedPayload) Validate() error {
	if p.Version == 0 {
		return fmt.Errorf("%w: version is zero", ErrOrderReceivedPayloadInvalid)
	}
	if p.TenantID == "" {
		return fmt.Errorf("%w: tenant_id missing", ErrOrderReceivedPayloadInvalid)
	}
	if p.OrderID == "" {
		return fmt.Errorf("%w: order_id missing", ErrOrderReceivedPayloadInvalid)
	}
	if p.IdempotencyKey == "" {
		return fmt.Errorf("%w: idempotency_key missing", ErrOrderReceivedPayloadInvalid)
	}
	if p.Channel == "" {
		return fmt.Errorf("%w: channel missing", ErrOrderReceivedPayloadInvalid)
	}
	if len(p.Items) == 0 {
		return fmt.Errorf("%w: at least one item required", ErrOrderReceivedPayloadInvalid)
	}
	return nil
}

func (p OrderReceivedPayload) asMap() map[string]any {
	items := make([]any, 0, len(p.Items))
	for _, line := range p.Items {
		items = append(items, map[string]any{
			"sku":          line.SKU,
			"quantity":     line.Quantity,
			"unit_cents":   line.UnitCents,
			"product_id":   line.ProductID,
			"warehouse_id": line.WarehouseID,
		})
	}
	return map[string]any{
		"version":         p.Version,
		"tenant_id":       p.TenantID,
		"order_id":        p.OrderID,
		"shop_id":         p.ShopID,
		"channel":         p.Channel,
		"buyer_email":     p.BuyerEmail,
		"total_cents":     p.TotalCents,
		"currency":        p.Currency,
		"items":           items,
		"status":          p.Status,
		"idempotency_key": p.IdempotencyKey,
		"occurred_at":     p.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
}

// NewOrderReceivedEvent is the canonical constructor.
func NewOrderReceivedEvent(source string, occurredAt time.Time, payload OrderReceivedPayload) (Event, error) {
	if payload.Version == 0 {
		payload.Version = OrderReceivedPayloadVersion
	}
	if occurredAt.IsZero() {
		occurredAt = time.Now().UTC()
	}
	if payload.OccurredAt.IsZero() {
		payload.OccurredAt = occurredAt
	}
	if err := payload.Validate(); err != nil {
		return Event{}, err
	}
	if source == "" {
		source = "webhook.tiktok.order"
	}
	return Event{
		Type:      OrderReceived,
		TenantID:  payload.TenantID,
		Payload:   payload.asMap(),
		Timestamp: occurredAt.UTC(),
		Source:    source,
	}, nil
}
