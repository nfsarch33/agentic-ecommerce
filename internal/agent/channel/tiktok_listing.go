// File scope: v3.3.0 EC-3-2 TikTok product listing agent.
//
// The agent subscribes to ProductEnrichedEvent on the existing
// eventbus, adapts the enriched product into the TikTok Shop wire
// format (category mapping, shipping template id, video SKU), and
// invokes the EC-3-1 social.Client to publish. On API failure the
// compensating delete fires and a TikTokListingRolledBack event is
// emitted so dashboards + the EC-7-5 returns workflow can react.
//
// Decomposition: the heavy lifting splits into adaptPayload (pure;
// no IO), publishWithRollback (IO + saga), and HandleEvent (envelope
// gate). Per-function cyclomatic stays under 6.
package channel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// CategoryMapper maps an internal category id (or path) to the
// TikTok Shop category id. The composition root supplies the
// concrete map; the in-package default returns the input unchanged
// so unit tests need no fixture.
type CategoryMapper func(internalCategory string) string

// ShippingTemplateResolver returns the operator-configured
// shipping_template_id for a tenant + category. Pure for testability.
type ShippingTemplateResolver func(tenantID, internalCategory string) string

// TikTokListingMetrics is the small port the listing agent emits
// publish + rollback counters through. Mirrors social.TikTokMetricsHook
// scope so the same observability struct can satisfy both.
type TikTokListingMetrics interface {
	RecordListing(tenantID, outcome string)
}

// TikTokListingConfig wires a TikTokListingAgent.
type TikTokListingConfig struct {
	Client            social.Client
	Publisher         eventbus.Publisher
	Consumer          eventbus.Consumer
	TenantID          string
	CategoryMapper    CategoryMapper
	ShippingResolver  ShippingTemplateResolver
	DefaultShipping   string
	Metrics           TikTokListingMetrics
	Now               func() time.Time
	SubscriptionGroup string
}

// TikTokListingAgent is the v3.3.0 EC-3-2 publish agent.
type TikTokListingAgent struct {
	cfg     TikTokListingConfig
	logger  *slog.Logger
	mapper  CategoryMapper
	shipper ShippingTemplateResolver
	now     func() time.Time

	mu     sync.Mutex
	closed bool
}

// NewTikTokListingAgent constructs an agent.
func NewTikTokListingAgent(logger *slog.Logger, cfg TikTokListingConfig) (*TikTokListingAgent, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Client == nil {
		return nil, fmt.Errorf("%w: social.Client required", ErrChannelUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: eventbus.Publisher required", ErrChannelUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrChannelUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	mapper := cfg.CategoryMapper
	if mapper == nil {
		mapper = func(c string) string { return c }
	}
	shipper := cfg.ShippingResolver
	if shipper == nil {
		shipper = func(_, _ string) string { return cfg.DefaultShipping }
	}
	if cfg.SubscriptionGroup == "" {
		cfg.SubscriptionGroup = "channel.tiktok.listing"
	}
	return &TikTokListingAgent{
		cfg:     cfg,
		logger:  logger,
		mapper:  mapper,
		shipper: shipper,
		now:     cfg.Now,
	}, nil
}

// Start subscribes to ProductEnrichedEvent on the supplied
// Consumer. Returns the Subscribe error directly so the composition
// root can fail boot when the bus is down.
func (a *TikTokListingAgent) Start(ctx context.Context) error {
	if a.cfg.Consumer == nil {
		return fmt.Errorf("%w: Consumer required for Start", ErrChannelUnconfigured)
	}
	return a.cfg.Consumer.Subscribe(ctx, []eventbus.EventType{eventbus.ProductEnriched}, a.cfg.SubscriptionGroup, a.HandleEvent)
}

// Close marks the agent closed.
func (a *TikTokListingAgent) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// HandleEvent is the eventbus dispatch entry point. Decodes the
// payload, gates on tenant scope, and delegates to publishWithRollback.
func (a *TikTokListingAgent) HandleEvent(ctx context.Context, evt eventbus.Event) error {
	if err := a.guard(); err != nil {
		return err
	}
	payload, err := decodeEnriched(evt)
	if err != nil {
		return err
	}
	if payload.TenantID != a.cfg.TenantID {
		return fmt.Errorf("%w: event=%s agent=%s", ErrChannelTenantMismatch, payload.TenantID, a.cfg.TenantID)
	}
	return a.publishWithRollback(ctx, payload)
}

// publishWithRollback invokes the social.Client and runs the
// compensating action on failure. Saga emit is best-effort; the
// caller still sees the publish error so the eventbus retry
// machinery can replay.
func (a *TikTokListingAgent) publishWithRollback(ctx context.Context, payload eventbus.ProductEnrichedPayload) error {
	tikTokPayload := a.adaptPayload(payload)
	remoteID, publishErr := a.cfg.Client.CreateProduct(ctx, tikTokPayload)
	if publishErr == nil {
		a.recordListing("published")
		return nil
	}
	a.logger.Warn("channel.tiktok.publish_failed", "tenant_id", payload.TenantID, "product_id", payload.ProductID, "error", publishErr)
	a.recordListing("publish_failed")
	a.runRollback(ctx, payload, remoteID, publishErr)
	return fmt.Errorf("tiktok publish: %w", publishErr)
}

// runRollback fires the compensating delete (if a remoteID was
// observed) and emits TikTokListingRolledBack. Both steps are
// best-effort so the original publish error stays the primary signal.
func (a *TikTokListingAgent) runRollback(ctx context.Context, payload eventbus.ProductEnrichedPayload, remoteID string, publishErr error) {
	stage := "create_product"
	if remoteID != "" {
		if delErr := a.cfg.Client.DeleteProduct(ctx, remoteID); delErr != nil {
			a.logger.Error("channel.tiktok.rollback_failed", "tenant_id", payload.TenantID, "product_id", payload.ProductID, "remote_id", remoteID, "error", delErr)
			stage = "delete_product"
		} else {
			stage = "compensated"
		}
	}
	rollback := eventbus.TikTokListingRollbackPayload{
		TenantID:   payload.TenantID,
		ProductID:  payload.ProductID,
		RemoteID:   remoteID,
		Reason:     publishErr.Error(),
		Stage:      stage,
		OccurredAt: a.now().UTC().Format(time.RFC3339Nano),
	}
	evt, err := eventbus.NewTikTokListingRollbackEvent("agent.channel.tiktok", a.now(), rollback)
	if err != nil {
		a.logger.Error("channel.tiktok.rollback_event_invalid", "error", err)
		return
	}
	if err := a.cfg.Publisher.Publish(ctx, evt); err != nil {
		a.logger.Error("channel.tiktok.rollback_event_publish_failed", "error", err)
	}
	a.recordListing("rolled_back")
}

func (a *TikTokListingAgent) recordListing(outcome string) {
	if a.cfg.Metrics == nil {
		return
	}
	a.cfg.Metrics.RecordListing(a.cfg.TenantID, outcome)
}

func (a *TikTokListingAgent) guard() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrChannelClosed
	}
	return nil
}

// adaptPayload is the pure mapping function; no IO, no errors.
// Centralised here so EC-3-2 tests can table-drive it.
func (a *TikTokListingAgent) adaptPayload(p eventbus.ProductEnrichedPayload) social.TikTokProductPayload {
	return social.TikTokProductPayload{
		TenantID:         p.TenantID,
		ExternalID:       p.ExternalID,
		Title:            p.EnglishTitle,
		Description:      p.EnglishDescription,
		CategoryID:       a.mapper(p.CategoryID),
		BrandName:        p.BrandName,
		PriceCents:       p.PriceCents,
		Currency:         p.Currency,
		StockUnits:       p.StockUnits,
		ShippingTemplate: a.resolveShipping(p),
		Images:           p.Images,
		VideoSKUURL:      p.VideoSKUURL,
		SellerSKU:        p.SellerSKU,
		WarehouseID:      p.WarehouseID,
	}
}

func (a *TikTokListingAgent) resolveShipping(p eventbus.ProductEnrichedPayload) string {
	if p.ShippingTemplate != "" {
		return p.ShippingTemplate
	}
	resolved := a.shipper(p.TenantID, p.CategoryID)
	if resolved != "" {
		return resolved
	}
	return a.cfg.DefaultShipping
}

// decodeEnriched extracts a typed ProductEnrichedPayload from a raw
// Event.Payload map. Mirrors the v2.4 envelope unmarshal pattern
// used elsewhere in the eventbus.
func decodeEnriched(evt eventbus.Event) (eventbus.ProductEnrichedPayload, error) {
	if evt.Type != eventbus.ProductEnriched {
		return eventbus.ProductEnrichedPayload{}, fmt.Errorf("%w: type=%s", ErrChannelEnvelopeInvalid, evt.Type)
	}
	payload, err := decodePayloadMap(evt.Payload)
	if err != nil {
		return eventbus.ProductEnrichedPayload{}, err
	}
	if payload.TenantID == "" {
		payload.TenantID = evt.TenantID
	}
	if err := payload.Validate(); err != nil {
		return eventbus.ProductEnrichedPayload{}, fmt.Errorf("%w: %v", ErrChannelEnvelopeInvalid, err)
	}
	return payload, nil
}

// decodePayloadMap is the reflection-free Event.Payload extractor.
// Each field is read with a typed helper so missing values surface
// as zero-values rather than panics.
func decodePayloadMap(m map[string]any) (eventbus.ProductEnrichedPayload, error) {
	if m == nil {
		return eventbus.ProductEnrichedPayload{}, fmt.Errorf("%w: payload nil", ErrChannelEnvelopeInvalid)
	}
	out := eventbus.ProductEnrichedPayload{
		Version:            intField(m, "version"),
		TenantID:           stringField(m, "tenant_id"),
		ProductID:          stringField(m, "product_id"),
		ExternalID:         stringField(m, "external_id"),
		EnglishTitle:       stringField(m, "english_title"),
		EnglishDescription: stringField(m, "english_description"),
		CategoryID:         stringField(m, "category_id"),
		BrandName:          stringField(m, "brand_name"),
		PriceCents:         intField(m, "price_cents"),
		Currency:           stringField(m, "currency"),
		StockUnits:         intField(m, "stock_units"),
		ShippingTemplate:   stringField(m, "shipping_template"),
		VideoSKUURL:        stringField(m, "video_sku_url"),
		SellerSKU:          stringField(m, "seller_sku"),
		WarehouseID:        stringField(m, "warehouse_id"),
		QualityScore:       floatField(m, "quality_score"),
		Source:             stringField(m, "source"),
		Images:             stringSliceField(m, "images"),
	}
	return out, nil
}

func stringField(m map[string]any, key string) string {
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func intField(m map[string]any, key string) int {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int32:
		return int(n)
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}

func floatField(m map[string]any, key string) float64 {
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case float64:
		return n
	case float32:
		return float64(n)
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return 0
}

func stringSliceField(m map[string]any, key string) []string {
	raw, ok := m[key]
	if !ok || raw == nil {
		return nil
	}
	switch s := raw.(type) {
	case []string:
		return append([]string(nil), s...)
	case []any:
		out := make([]string, 0, len(s))
		for _, v := range s {
			if str, ok := v.(string); ok {
				out = append(out, str)
			}
		}
		return out
	}
	return nil
}

// errIs is a tiny convenience used by tests + the agent to branch on
// the social typed sentinels without importing them everywhere. Kept
// package-private so it cannot leak into the public surface.
func errIs(err, target error) bool { return errors.Is(err, target) }
