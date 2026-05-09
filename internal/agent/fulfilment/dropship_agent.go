// File scope: v3.5.0 EC-7-2 drop-ship supplier order agent.
//
// The agent subscribes to OrderNormalisedEvent (v3.5.0 EC-7-1),
// resolves the source supplier (1688/Taobao), and autonomously
// places drop-ship orders on customer-order receipt. Operator
// approval gate fires for orders > A$500 (configurable). Saga
// rollback runs when EVERY supplier adapter (primary + fallback)
// fails -- the v3.3.0 EC-3-4 inventory-sync compensating-action
// pattern is reused here.
//
// Reuse evidence:
//   - The v3.1.0 China adapters (china.Source) for supplier
//     identification.
//   - The v3.3.0 EC-3-4 saga rollback pattern (compensating
//     action + typed sentinel).
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//   - The fan-out-on-primary-fail-then-fallback pattern mirrors
//     v3.4.0 EC-4-3 channel router DLQ flow.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 7-sprint streak; v3.5.0 sprint 8 target):
//   - Place (envelope -> validate -> approval gate -> primary ->
//     fallback -> emit/rollback)
//   - tryPrimary (helper; cyclomatic 4)
//   - tryFallback (helper; cyclomatic 4)
//   - emitPlaced / emitRollback / emitPendingApproval (pure event
//     emission helpers)
//   - HandleOrderNormalised (eventbus dispatch)
//
// Each helper stays under cyclomatic 6.
//
// Resilience pillar (v2.10 baseline):
//   - Implements lifecycle.Closer.
//   - Synchronous order placement; no raw goroutines.
//   - Errors typed + %w-wrapped via package sentinels.
//   - Tenant-aware: every event carries TenantID.
//   - FulfilmentTrigger port owns the customer-side rollback so
//     the saga is composable with future Temporal wrappers.
package fulfilment

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// DefaultLargeOrderThresholdCents is the operator-approval gate
// threshold (A$500 = 50000 cents per the EC-7-2 spec).
const DefaultLargeOrderThresholdCents = 50000

// EC-7-2 typed sentinels.
var (
	// ErrDropshipAgentUnconfigured is returned when a required
	// dependency (TenantID / Primary / Publisher) is missing.
	ErrDropshipAgentUnconfigured = errors.New("fulfilment: dropship agent unconfigured")

	// ErrDropshipAgentClosed is returned by Place / HandleEvent
	// after Close.
	ErrDropshipAgentClosed = errors.New("fulfilment: dropship agent closed")

	// ErrInvalidNormalisedOrder is returned when the input order
	// fails the validate gate.
	ErrInvalidNormalisedOrder = errors.New("fulfilment: invalid normalised order")

	// ErrLargeOrderApprovalRequired is the sentinel for orders
	// above the configurable threshold. The result struct carries
	// PendingApproval=true; the typed sentinel is here for callers
	// that want errors.Is.
	ErrLargeOrderApprovalRequired = errors.New("fulfilment: large order approval required")

	// ErrAllSuppliersFailed is returned when every supplier adapter
	// (primary + fallback) failed AND the customer-side fulfillment
	// trigger was rolled back.
	ErrAllSuppliersFailed = errors.New("fulfilment: all suppliers failed; saga rolled back")

	// ErrDropshipSagaRolledBack is the sentinel for individual
	// rollback paths (primary failed + no fallback configured;
	// fallback rollback only). Wraps the underlying cause.
	ErrDropshipSagaRolledBack = errors.New("fulfilment: dropship saga rolled back")
)

// NormalisedOrderLine is the per-line structure of a customer
// order normalised by the EC-7-1 workflow. Pure value type.
type NormalisedOrderLine struct {
	SKU       string
	Quantity  int
	UnitCents int
	ProductID string
}

// NormalisedOrder is the input shape consumed by Place. Mirrors
// workflow.OrderNormalised + eventbus.OrderNormalisedPayload so
// the eventbus dispatcher can map 1:1.
type NormalisedOrder struct {
	TenantID        string
	OrderID         string
	ExternalOrderID string
	Channel         string
	BuyerEmail      string
	TotalAUDCents   int
	Currency        string
	Items           []NormalisedOrderLine
}

// SupplierOrderRequest is the unit of work submitted to a
// SupplierOrderClient.
type SupplierOrderRequest struct {
	TenantID      string
	OrderID       string
	BuyerEmail    string
	TotalAUDCents int
	Items         []NormalisedOrderLine
}

// SupplierOrderResult is the supplier-side acknowledgement.
type SupplierOrderResult struct {
	SupplierOrderID string
	PlacedAt        time.Time
}

// SupplierOrderClient is the small port both supplier adapters
// (primary 1688/Taobao + AliExpress fallback) implement.
type SupplierOrderClient interface {
	SupplierName() string
	PlaceOrder(ctx context.Context, req SupplierOrderRequest) (SupplierOrderResult, error)
}

// FulfilmentTrigger is the port the agent uses to fire (or roll
// back) the customer-side fulfillment workflow. Implementations
// can be in-memory (tests), or wrap a Temporal signal.
type FulfilmentTrigger interface {
	// Trigger fires the customer-side fulfillment for the given
	// order id. Called when the supplier adapter succeeded.
	Trigger(ctx context.Context, orderID string) error
	// Rollback runs the compensating action when every supplier
	// failed (the customer-side fulfillment must NOT fire).
	Rollback(ctx context.Context, orderID string) error
}

// DropshipAgentMetrics is the small port the agent emits the
// dropship_orders_total counter through.
type DropshipAgentMetrics interface {
	RecordDropshipOrder(tenantID, supplier, status string)
}

// DropshipAgentKPISample is the EvoMap KPI sample emitted per
// Place call.
type DropshipAgentKPISample struct {
	TenantID string
	Supplier string
	Status   string // placed | approval_pending | rolled_back
}

// DropshipAgentKPIHook is the optional EvoMap emission hook.
type DropshipAgentKPIHook func(DropshipAgentKPISample)

// DropshipAgentConfig wires a DropshipAgent.
type DropshipAgentConfig struct {
	TenantID                 string
	Primary                  SupplierOrderClient
	Fallback                 SupplierOrderClient
	Publisher                eventbus.Publisher
	FulfilmentTrigger        FulfilmentTrigger
	LargeOrderThresholdCents int
	Metrics                  DropshipAgentMetrics
	KPIHook                  DropshipAgentKPIHook
	Now                      func() time.Time
}

// DropshipResult is the agent's per-Place output.
type DropshipResult struct {
	OrderID         string
	Supplier        string
	SupplierOrderID string
	Placed          bool
	PendingApproval bool
	SagaRolledBack  bool
	GeneratedAt     time.Time
}

// DropshipAgent is the v3.5.0 EC-7-2 agent.
type DropshipAgent struct {
	cfg    DropshipAgentConfig
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewDropshipAgent constructs an agent.
func NewDropshipAgent(logger *slog.Logger, cfg DropshipAgentConfig) (*DropshipAgent, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateDropshipConfig(cfg); err != nil {
		return nil, err
	}
	applyDropshipDefaults(&cfg)
	return &DropshipAgent{cfg: cfg, logger: logger}, nil
}

func validateDropshipConfig(cfg DropshipAgentConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrDropshipAgentUnconfigured)
	}
	if cfg.Primary == nil {
		return fmt.Errorf("%w: Primary supplier required", ErrDropshipAgentUnconfigured)
	}
	if cfg.Publisher == nil {
		return fmt.Errorf("%w: Publisher required", ErrDropshipAgentUnconfigured)
	}
	return nil
}

func applyDropshipDefaults(cfg *DropshipAgentConfig) {
	if cfg.LargeOrderThresholdCents <= 0 {
		cfg.LargeOrderThresholdCents = DefaultLargeOrderThresholdCents
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
}

// Close marks the agent closed.
func (a *DropshipAgent) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// LargeOrderThresholdCents returns the configured approval gate.
func (a *DropshipAgent) LargeOrderThresholdCents() int { return a.cfg.LargeOrderThresholdCents }

// Place runs the EC-7-2 pipeline:
// validate -> approval gate -> primary -> fallback -> emit/rollback.
//
// Decomposition keeps cyclomatic per-function under 6.
func (a *DropshipAgent) Place(ctx context.Context, order NormalisedOrder) (DropshipResult, error) {
	if err := a.guard(); err != nil {
		return DropshipResult{}, err
	}
	if err := validateNormalisedOrder(order); err != nil {
		return DropshipResult{}, err
	}
	if order.TotalAUDCents > a.cfg.LargeOrderThresholdCents {
		return a.routeApproval(ctx, order), nil
	}
	res, err := a.tryPrimary(ctx, order)
	if err == nil {
		return res, nil
	}
	a.logger.Warn("fulfilment.dropship.primary_failed", "tenant_id", a.cfg.TenantID, "order_id", order.OrderID, "supplier", a.cfg.Primary.SupplierName(), "error", err)
	res, err = a.tryFallback(ctx, order, err)
	if err == nil {
		return res, nil
	}
	return a.runSagaRollback(ctx, order, err), err
}

// routeApproval emits the LargeDropshipOrderPendingApproval event
// and returns the result struct. No supplier call fires.
func (a *DropshipAgent) routeApproval(ctx context.Context, order NormalisedOrder) DropshipResult {
	res := DropshipResult{
		OrderID:         order.OrderID,
		PendingApproval: true,
		GeneratedAt:     a.cfg.Now(),
	}
	a.emitPendingApproval(ctx, order)
	a.recordOutcome(order, "approval_pending", "")
	return res
}

// tryPrimary calls the primary supplier and emits DropshipOrderPlaced
// on success. Cyclomatic 4.
func (a *DropshipAgent) tryPrimary(ctx context.Context, order NormalisedOrder) (DropshipResult, error) {
	supplierName := a.cfg.Primary.SupplierName()
	resp, err := a.cfg.Primary.PlaceOrder(ctx, supplierRequestFromOrder(order))
	if err != nil {
		return DropshipResult{OrderID: order.OrderID, GeneratedAt: a.cfg.Now()}, err
	}
	res := DropshipResult{
		OrderID:         order.OrderID,
		Supplier:        supplierName,
		SupplierOrderID: resp.SupplierOrderID,
		Placed:          true,
		GeneratedAt:     a.cfg.Now(),
	}
	a.fireFulfilment(ctx, order.OrderID)
	a.emitPlaced(ctx, order, supplierName, resp.SupplierOrderID)
	a.recordOutcome(order, "placed", supplierName)
	return res, nil
}

// tryFallback calls the fallback supplier (when configured) and
// emits DropshipOrderPlaced on success. Cyclomatic 4.
func (a *DropshipAgent) tryFallback(ctx context.Context, order NormalisedOrder, primaryErr error) (DropshipResult, error) {
	if a.cfg.Fallback == nil {
		return DropshipResult{}, fmt.Errorf("%w: primary err=%v; no fallback configured", ErrAllSuppliersFailed, primaryErr)
	}
	supplierName := a.cfg.Fallback.SupplierName()
	resp, err := a.cfg.Fallback.PlaceOrder(ctx, supplierRequestFromOrder(order))
	if err != nil {
		return DropshipResult{}, fmt.Errorf("%w: primary err=%v fallback err=%v", ErrAllSuppliersFailed, primaryErr, err)
	}
	res := DropshipResult{
		OrderID:         order.OrderID,
		Supplier:        supplierName,
		SupplierOrderID: resp.SupplierOrderID,
		Placed:          true,
		GeneratedAt:     a.cfg.Now(),
	}
	a.fireFulfilment(ctx, order.OrderID)
	a.emitPlaced(ctx, order, supplierName, resp.SupplierOrderID)
	a.recordOutcome(order, "placed", supplierName)
	return res, nil
}

// runSagaRollback runs the customer-side fulfillment rollback (no
// fulfillment fired before saga rollback in v3.5.0; the trigger
// only fires on supplier success). Emits DropshipOrderRolledBack.
func (a *DropshipAgent) runSagaRollback(ctx context.Context, order NormalisedOrder, allFailedErr error) DropshipResult {
	if a.cfg.FulfilmentTrigger != nil {
		if err := a.cfg.FulfilmentTrigger.Rollback(ctx, order.OrderID); err != nil {
			a.logger.Error("fulfilment.dropship.rollback_failed", "tenant_id", a.cfg.TenantID, "order_id", order.OrderID, "error", err)
		}
	}
	a.emitRollback(ctx, order, allFailedErr)
	a.recordOutcome(order, "rolled_back", "")
	return DropshipResult{
		OrderID:        order.OrderID,
		SagaRolledBack: true,
		GeneratedAt:    a.cfg.Now(),
	}
}

// fireFulfilment runs the customer-side fulfillment trigger
// (best-effort; failures are logged but do not surface as the
// primary error since the supplier order succeeded).
func (a *DropshipAgent) fireFulfilment(ctx context.Context, orderID string) {
	if a.cfg.FulfilmentTrigger == nil {
		return
	}
	if err := a.cfg.FulfilmentTrigger.Trigger(ctx, orderID); err != nil {
		a.logger.Error("fulfilment.dropship.trigger_failed", "tenant_id", a.cfg.TenantID, "order_id", orderID, "error", err)
	}
}

// HandleOrderNormalised dispatches an OrderNormalisedEvent to
// Place. Mirrors the v3.3.0 EC-3-2 channel.HandleEvent pattern.
func (a *DropshipAgent) HandleOrderNormalised(ctx context.Context, evt eventbus.Event) error {
	if err := a.guard(); err != nil {
		return err
	}
	if evt.Type != eventbus.OrderNormalised {
		return fmt.Errorf("fulfilment: unexpected event type %s", evt.Type)
	}
	if evt.TenantID != a.cfg.TenantID {
		return fmt.Errorf("fulfilment: tenant mismatch event=%s agent=%s", evt.TenantID, a.cfg.TenantID)
	}
	order, err := decodeNormalisedOrder(evt)
	if err != nil {
		return err
	}
	_, err = a.Place(ctx, order)
	if errors.Is(err, ErrAllSuppliersFailed) {
		// Saga already rolled back; the upstream subscriber only
		// needs to know the agent handled this event without crash.
		return nil
	}
	return err
}

func (a *DropshipAgent) guard() error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrDropshipAgentClosed
	}
	return nil
}

func (a *DropshipAgent) emitPlaced(ctx context.Context, order NormalisedOrder, supplier, supplierOrderID string) {
	payload := eventbus.DropshipOrderPayload{
		Version:         eventbus.DropshipOrderPayloadVersion,
		TenantID:        a.cfg.TenantID,
		OrderID:         order.OrderID,
		Supplier:        supplier,
		SupplierOrderID: supplierOrderID,
		TotalAUDCents:   order.TotalAUDCents,
		OccurredAt:      a.cfg.Now(),
	}
	evt, err := eventbus.NewDropshipOrderPlacedEvent("agent.fulfilment.dropship", a.cfg.Now(), payload)
	if err != nil {
		a.logger.Error("fulfilment.dropship.placed_event_invalid", "error", err)
		return
	}
	if err := a.cfg.Publisher.Publish(ctx, evt); err != nil {
		a.logger.Error("fulfilment.dropship.placed_publish_failed", "error", err)
	}
}

func (a *DropshipAgent) emitPendingApproval(ctx context.Context, order NormalisedOrder) {
	payload := eventbus.DropshipOrderPayload{
		Version:       eventbus.DropshipOrderPayloadVersion,
		TenantID:      a.cfg.TenantID,
		OrderID:       order.OrderID,
		Supplier:      a.cfg.Primary.SupplierName(),
		TotalAUDCents: order.TotalAUDCents,
		Reason:        fmt.Sprintf("total %d > threshold %d (A$%.2f)", order.TotalAUDCents, a.cfg.LargeOrderThresholdCents, float64(a.cfg.LargeOrderThresholdCents)/100),
		OccurredAt:    a.cfg.Now(),
	}
	evt, err := eventbus.NewLargeDropshipOrderPendingApprovalEvent("agent.fulfilment.dropship", a.cfg.Now(), payload)
	if err != nil {
		a.logger.Error("fulfilment.dropship.pending_event_invalid", "error", err)
		return
	}
	if err := a.cfg.Publisher.Publish(ctx, evt); err != nil {
		a.logger.Error("fulfilment.dropship.pending_publish_failed", "error", err)
	}
}

func (a *DropshipAgent) emitRollback(ctx context.Context, order NormalisedOrder, cause error) {
	payload := eventbus.DropshipOrderPayload{
		Version:       eventbus.DropshipOrderPayloadVersion,
		TenantID:      a.cfg.TenantID,
		OrderID:       order.OrderID,
		Supplier:      a.cfg.Primary.SupplierName(),
		TotalAUDCents: order.TotalAUDCents,
		Reason:        cause.Error(),
		OccurredAt:    a.cfg.Now(),
	}
	evt, err := eventbus.NewDropshipOrderRolledBackEvent("agent.fulfilment.dropship", a.cfg.Now(), payload)
	if err != nil {
		a.logger.Error("fulfilment.dropship.rolled_event_invalid", "error", err)
		return
	}
	if err := a.cfg.Publisher.Publish(ctx, evt); err != nil {
		a.logger.Error("fulfilment.dropship.rolled_publish_failed", "error", err)
	}
}

func (a *DropshipAgent) recordOutcome(order NormalisedOrder, status, supplier string) {
	if a.cfg.Metrics != nil {
		a.cfg.Metrics.RecordDropshipOrder(a.cfg.TenantID, supplier, status)
	}
	if a.cfg.KPIHook != nil {
		a.cfg.KPIHook(DropshipAgentKPISample{TenantID: a.cfg.TenantID, Supplier: supplier, Status: status})
	}
}

func validateNormalisedOrder(o NormalisedOrder) error {
	if strings.TrimSpace(o.TenantID) == "" {
		return fmt.Errorf("%w: tenant_id required", ErrInvalidNormalisedOrder)
	}
	if strings.TrimSpace(o.OrderID) == "" {
		return fmt.Errorf("%w: order_id required", ErrInvalidNormalisedOrder)
	}
	if strings.TrimSpace(o.Channel) == "" {
		return fmt.Errorf("%w: channel required", ErrInvalidNormalisedOrder)
	}
	if o.TotalAUDCents < 0 {
		return fmt.Errorf("%w: total_aud_cents cannot be negative", ErrInvalidNormalisedOrder)
	}
	if len(o.Items) == 0 {
		return fmt.Errorf("%w: at least one item required", ErrInvalidNormalisedOrder)
	}
	return nil
}

func supplierRequestFromOrder(order NormalisedOrder) SupplierOrderRequest {
	return SupplierOrderRequest{
		TenantID:      order.TenantID,
		OrderID:       order.OrderID,
		BuyerEmail:    order.BuyerEmail,
		TotalAUDCents: order.TotalAUDCents,
		Items:         order.Items,
	}
}

// decodeNormalisedOrder reads the typed OrderNormalisedPayload off
// the eventbus envelope. Mirrors the v3.4.0 channel.decodeEnriched
// pattern.
func decodeNormalisedOrder(evt eventbus.Event) (NormalisedOrder, error) {
	if evt.Payload == nil {
		return NormalisedOrder{}, fmt.Errorf("%w: payload nil", ErrInvalidNormalisedOrder)
	}
	out := NormalisedOrder{
		TenantID:        dropStringFromMap(evt.Payload, "tenant_id"),
		OrderID:         dropStringFromMap(evt.Payload, "order_id"),
		ExternalOrderID: dropStringFromMap(evt.Payload, "external_order_id"),
		Channel:         dropStringFromMap(evt.Payload, "channel"),
		BuyerEmail:      dropStringFromMap(evt.Payload, "buyer_email"),
		TotalAUDCents:   dropIntFromMap(evt.Payload, "total_aud_cents"),
		Currency:        dropStringFromMap(evt.Payload, "currency"),
	}
	if items, ok := evt.Payload["items"].([]any); ok {
		for _, raw := range items {
			if m, ok := raw.(map[string]any); ok {
				out.Items = append(out.Items, NormalisedOrderLine{
					SKU:       dropStringFromMap(m, "sku"),
					Quantity:  dropIntFromMap(m, "quantity"),
					UnitCents: dropIntFromMap(m, "unit_cents"),
					ProductID: dropStringFromMap(m, "product_id"),
				})
			}
		}
	}
	if err := validateNormalisedOrder(out); err != nil {
		return NormalisedOrder{}, err
	}
	return out, nil
}

func dropStringFromMap(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func dropIntFromMap(m map[string]any, key string) int {
	if m == nil {
		return 0
	}
	v, ok := m[key]
	if !ok {
		return 0
	}
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	}
	return 0
}
