// File scope: v3.5.0 EC-7-1 multi-channel order aggregator workflow.
//
// Aggregates orders from TikTok Shop (v3.3.0), Facebook Shop
// (v3.4.0), WooCommerce (existing), and future channels into a
// single normalised internal Order shape. Dedupes by
// (channel, external_order_id). Emits OrderNormalisedEvent for the
// EC-7-2 drop-ship agent + downstream fulfillment pipeline.
//
// Reuse evidence:
//   - testsuite.WorkflowTestSuite pattern from v2.2.0 membership +
//     v3.1.0 sourcing (sourcing_test.go).
//   - Idempotency-key dedup pattern from v3.3.0 EC-3-4
//     sync.MemoryIdempotencyStore.
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - The two-phase workflow shape (per-input activity + post-loop
//     summary) mirrors the existing sourcing.go composition.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 7-sprint streak; v3.5.0 sprint 8 target):
//   - OrderAggregatorWorkflow (envelope; per-order loop)
//   - normaliseEntry (single-order pipeline; cyclomatic 5)
//   - publishIfNew (dedup gate + publish)
//
// The workflow body itself is a linear loop with one branch per
// dedup outcome -- cyclomatic 4.
package workflow

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

// EC-7-1 activity names. Stable across replay so deterministic
// history fixtures stay valid.
const (
	NormaliseChannelOrderActivity  = "order_aggregator.normalise"
	DedupOrderActivity             = "order_aggregator.dedup"
	PublishOrderNormalisedActivity = "order_aggregator.publish"
)

// ChannelOrderLine is a single line item on a raw channel order.
type ChannelOrderLine struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int    `json:"unit_cents"`
	ProductID string `json:"product_id,omitempty"`
}

// ChannelOrderInput is the per-channel raw order payload submitted
// to the aggregator workflow. The shape is intentionally a
// superset of the v3.3.0 OrderReceivedPayload + the v3.4.0
// Facebook batch order so a future webhook adapter can map 1:1.
type ChannelOrderInput struct {
	TenantID        string             `json:"tenant_id"`
	Channel         string             `json:"channel"`
	ExternalOrderID string             `json:"external_order_id"`
	BuyerEmail      string             `json:"buyer_email,omitempty"`
	TotalCents      int                `json:"total_cents"`
	Currency        string             `json:"currency"`
	Status          string             `json:"status,omitempty"`
	ShippingCountry string             `json:"shipping_country,omitempty"`
	Items           []ChannelOrderLine `json:"items"`
	OccurredAt      time.Time          `json:"occurred_at"`
}

// OrderNormalisedLine mirrors the eventbus envelope. Pure value type.
type OrderNormalisedLine struct {
	SKU       string `json:"sku"`
	Quantity  int    `json:"quantity"`
	UnitCents int    `json:"unit_cents"`
	ProductID string `json:"product_id,omitempty"`
}

// OrderNormalised is the post-normalisation domain shape produced
// by the aggregator workflow. Matches eventbus.OrderNormalisedPayload
// 1:1 so the activity that publishes the event has no translation
// step.
type OrderNormalised struct {
	TenantID        string                `json:"tenant_id"`
	OrderID         string                `json:"order_id"`
	ExternalOrderID string                `json:"external_order_id"`
	Channel         string                `json:"channel"`
	BuyerEmail      string                `json:"buyer_email,omitempty"`
	TotalAUDCents   int                   `json:"total_aud_cents"`
	Currency        string                `json:"currency"`
	Status          string                `json:"status,omitempty"`
	ShippingCountry string                `json:"shipping_country,omitempty"`
	Items           []OrderNormalisedLine `json:"items"`
	OccurredAt      time.Time             `json:"occurred_at"`
}

// OrderAggregatorWorkflowInput is the workflow input.
type OrderAggregatorWorkflowInput struct {
	TenantID string              `json:"tenant_id"`
	Orders   []ChannelOrderInput `json:"orders"`
}

// OrderAggregatorWorkflowResult is the workflow result.
type OrderAggregatorWorkflowResult struct {
	TenantID   string            `json:"tenant_id"`
	Normalised int               `json:"normalised"`
	Duplicates int               `json:"duplicates"`
	Failures   int               `json:"failures"`
	Orders     []OrderNormalised `json:"orders"`
}

// OrderDedupStore is the small port the dedup activity consumes.
// Implementations may be in-memory (tests) or Postgres-backed (the
// migrations/0014 + sync idempotency store).
type OrderDedupStore interface {
	Reserve(ctx context.Context, key string) (bool, error)
}

// OrderAggregatorMetrics is the small port the activity layer
// emits ec_order_aggregator_normalisations_total through. Mirrors
// the EC-3 TikTokMetricsHook + EC-4-3 RouterMetrics pattern so the
// composition root can wire either an observability adapter or a
// recording test double.
type OrderAggregatorMetrics interface {
	RecordOrderNormalisation(tenantID, channel, status string)
}

// OrderAggregatorActivityDeps wires concrete dependencies into the
// activity struct.
type OrderAggregatorActivityDeps struct {
	Publisher  eventbus.Publisher
	DedupStore OrderDedupStore
	Metrics    OrderAggregatorMetrics
	Now        func() time.Time
}

// OrderAggregatorActivities is the activity struct registered with
// the worker.
type OrderAggregatorActivities struct {
	publisher  eventbus.Publisher
	dedupStore OrderDedupStore
	metrics    OrderAggregatorMetrics
	now        func() time.Time
}

// NewOrderAggregatorActivities constructs the activity struct.
func NewOrderAggregatorActivities(deps OrderAggregatorActivityDeps) *OrderAggregatorActivities {
	now := deps.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	return &OrderAggregatorActivities{
		publisher:  deps.Publisher,
		dedupStore: deps.DedupStore,
		metrics:    deps.Metrics,
		now:        now,
	}
}

// OrderAggregatorWorkflow is the v3.5.0 EC-7-1 Temporal workflow.
//
// Decomposition: the per-order body splits into normaliseEntry +
// publishIfNew so the workflow loop stays cyclomatic 4.
func OrderAggregatorWorkflow(ctx temporalworkflow.Context, input OrderAggregatorWorkflowInput) (OrderAggregatorWorkflowResult, error) {
	activityOptions := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, activityOptions)
	result := OrderAggregatorWorkflowResult{TenantID: input.TenantID}
	for _, order := range input.Orders {
		normalised, err := normaliseEntry(ctx, order)
		if err != nil {
			result.Failures++
			continue
		}
		fired, err := publishIfNew(ctx, normalised)
		if err != nil {
			result.Failures++
			continue
		}
		if !fired {
			result.Duplicates++
			continue
		}
		result.Normalised++
		result.Orders = append(result.Orders, normalised)
	}
	return result, nil
}

// normaliseEntry runs the per-order normalisation activity.
// Pulled out so the workflow body stays small.
func normaliseEntry(ctx temporalworkflow.Context, in ChannelOrderInput) (OrderNormalised, error) {
	var out OrderNormalised
	if err := temporalworkflow.ExecuteActivity(ctx, NormaliseChannelOrderActivity, in).Get(ctx, &out); err != nil {
		return OrderNormalised{}, err
	}
	return out, nil
}

// publishIfNew runs the dedup gate; if the order is fresh, runs
// the publish activity. Returns (fired, err) -- fired=false means
// the dedup gate rejected the duplicate.
func publishIfNew(ctx temporalworkflow.Context, normalised OrderNormalised) (bool, error) {
	var fresh bool
	if err := temporalworkflow.ExecuteActivity(ctx, DedupOrderActivity, normalised).Get(ctx, &fresh); err != nil {
		return false, err
	}
	if !fresh {
		return false, nil
	}
	if err := temporalworkflow.ExecuteActivity(ctx, PublishOrderNormalisedActivity, normalised).Get(ctx, nil); err != nil {
		return false, err
	}
	return true, nil
}

// NormaliseChannelOrder is the activity that maps a ChannelOrderInput
// to an OrderNormalised. Pure transformation; runs in the worker.
func (a *OrderAggregatorActivities) NormaliseChannelOrder(_ context.Context, in ChannelOrderInput) (OrderNormalised, error) {
	if err := validateChannelOrderInput(in); err != nil {
		return OrderNormalised{}, err
	}
	items := make([]OrderNormalisedLine, 0, len(in.Items))
	for _, line := range in.Items {
		items = append(items, OrderNormalisedLine{
			SKU:       line.SKU,
			Quantity:  line.Quantity,
			UnitCents: line.UnitCents,
			ProductID: line.ProductID,
		})
	}
	return OrderNormalised{
		TenantID:        in.TenantID,
		OrderID:         buildInternalOrderID(in.Channel, in.ExternalOrderID),
		ExternalOrderID: in.ExternalOrderID,
		Channel:         in.Channel,
		BuyerEmail:      in.BuyerEmail,
		TotalAUDCents:   in.TotalCents,
		Currency:        in.Currency,
		Status:          in.Status,
		ShippingCountry: in.ShippingCountry,
		Items:           items,
		OccurredAt:      in.OccurredAt,
	}, nil
}

// DedupOrder is the activity that consults the dedup store and
// returns true when the order is a fresh observation.
func (a *OrderAggregatorActivities) DedupOrder(ctx context.Context, normalised OrderNormalised) (bool, error) {
	if a.dedupStore == nil {
		// No store wired: treat every order as fresh. Production
		// composition root MUST wire a store (defensive fallback).
		return true, nil
	}
	key := buildDedupKey(normalised)
	return a.dedupStore.Reserve(ctx, key)
}

// PublishOrderNormalised is the activity that emits the typed
// OrderNormalisedEvent on the supplied eventbus publisher and
// records the metric.
func (a *OrderAggregatorActivities) PublishOrderNormalised(ctx context.Context, normalised OrderNormalised) error {
	if a.publisher == nil {
		a.recordNormalisation(normalised, "skipped")
		return nil
	}
	payload := eventbus.OrderNormalisedPayload{
		Version:         eventbus.OrderNormalisedPayloadVersion,
		TenantID:        normalised.TenantID,
		OrderID:         normalised.OrderID,
		ExternalOrderID: normalised.ExternalOrderID,
		Channel:         normalised.Channel,
		BuyerEmail:      normalised.BuyerEmail,
		TotalAUDCents:   normalised.TotalAUDCents,
		Currency:        normalised.Currency,
		Status:          normalised.Status,
		ShippingCountry: normalised.ShippingCountry,
		Items:           toEventbusLines(normalised.Items),
		OccurredAt:      normalised.OccurredAt,
	}
	evt, err := eventbus.NewOrderNormalisedEvent("workflow.order_aggregator", a.now(), payload)
	if err != nil {
		a.recordNormalisation(normalised, "failure")
		return fmt.Errorf("order_aggregator: build event: %w", err)
	}
	if err := a.publisher.Publish(ctx, evt); err != nil {
		a.recordNormalisation(normalised, "failure")
		return fmt.Errorf("order_aggregator: publish: %w", err)
	}
	a.recordNormalisation(normalised, "ok")
	return nil
}

func (a *OrderAggregatorActivities) recordNormalisation(o OrderNormalised, status string) {
	if a.metrics == nil {
		return
	}
	a.metrics.RecordOrderNormalisation(o.TenantID, o.Channel, status)
}

// validateChannelOrderInput enforces the activity input contract.
// Cyclomatic stays at 5.
func validateChannelOrderInput(in ChannelOrderInput) error {
	if strings.TrimSpace(in.TenantID) == "" {
		return ErrOrderTenantRequired
	}
	if strings.TrimSpace(in.Channel) == "" {
		return ErrOrderChannelRequired
	}
	if strings.TrimSpace(in.ExternalOrderID) == "" {
		return ErrOrderExternalIDMissing
	}
	if len(in.Items) == 0 {
		return ErrOrderNoLineItems
	}
	if in.OccurredAt.IsZero() {
		return ErrOrderOccurredAtMissing
	}
	return nil
}

// buildInternalOrderID composes the canonical internal order id.
// The prefix avoids collision with the per-channel external id
// across the aggregator + downstream consumers.
func buildInternalOrderID(channel, externalID string) string {
	return fmt.Sprintf("ord-%s-%s", channel, externalID)
}

// buildDedupKey is the canonical dedup key; mirrors the v3.3.0
// EC-3-4 IdempotencyStore key shape but tenant-prefixed.
func buildDedupKey(o OrderNormalised) string {
	return o.TenantID + "\x00" + o.Channel + "\x00" + o.ExternalOrderID
}

// toEventbusLines converts the activity-side line items to the
// eventbus envelope shape.
func toEventbusLines(in []OrderNormalisedLine) []eventbus.OrderNormalisedLine {
	out := make([]eventbus.OrderNormalisedLine, 0, len(in))
	for _, line := range in {
		out = append(out, eventbus.OrderNormalisedLine{
			SKU:       line.SKU,
			Quantity:  line.Quantity,
			UnitCents: line.UnitCents,
			ProductID: line.ProductID,
		})
	}
	return out
}
