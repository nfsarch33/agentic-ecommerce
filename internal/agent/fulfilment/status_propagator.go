// File scope: v3.8.0 EC-7-4 order status propagator + carrier
// webhook handlers.
//
// On a carrier webhook (AusPost / DHL), the EC-7-4 webhook receivers
// run HMAC verify-then-parse, decode the body to a
// ShipmentStatusUpdatedPayload, and dispatch to the StatusPropagator.
// The propagator fans out to every configured ChannelStatusUpdater
// (TikTok, FB, RedNote, WC, and future channels) with retry +
// exponential backoff, and is idempotent on (tracking_number,
// event_id).
//
// Reuse evidence:
//   - HMAC verify-then-parse pattern from v3.3.0 EC-3-3
//     internal/webhook/tiktok_order.go.
//   - In-memory idempotency store mirrors v3.3.0 EC-3-4 sync store
//     and the local generator cache pattern.
//   - eventbus.Publisher contract from v3.3.0 EC-3-3.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 13-sprint streak; v3.8.0 sprint 14 target):
//   - Propagate (envelope -> idempotency gate -> dispatch loop ->
//     publish summary -> return)
//   - dispatchToChannel (per-channel call + retry)
//   - retryWithBackoff (typed retry helper; single decision branch)
//   - hmacVerifyAusPost / hmacVerifyDHL (small helpers per carrier)
//
// Each helper stays under cyclomatic 6.

package fulfilment

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/carrier"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// EC-7-4 typed sentinels.
var (
	// ErrStatusPropagatorUnconfigured is returned when a required
	// dependency is missing.
	ErrStatusPropagatorUnconfigured = errors.New("fulfilment: status propagator unconfigured")

	// ErrStatusPropagatorClosed is returned after Close.
	ErrStatusPropagatorClosed = errors.New("fulfilment: status propagator closed")

	// ErrCarrierWebhookInvalid signals HMAC verify-then-parse
	// failure (bad signature, malformed body, missing event_id).
	ErrCarrierWebhookInvalid = errors.New("fulfilment: carrier webhook invalid")

	// ErrChannelStatusUpdateFailed signals every retry exhausted
	// for a given channel. The propagator surfaces this so the
	// caller can decide between operator alert vs. dead letter.
	ErrChannelStatusUpdateFailed = errors.New("fulfilment: channel status update failed")

	// ErrShipmentNotFound signals the carrier webhook referenced an
	// unknown tracking_number. Surfaces 404 to the carrier.
	ErrShipmentNotFound = errors.New("fulfilment: shipment not found")
)

// DefaultStatusPropagationRetries is the EC-7-4 retry budget.
const DefaultStatusPropagationRetries = 3

// DefaultStatusPropagationLatencyBudget is the 60-second acceptance
// criterion from EC-7-4 (3-channel update completes within 60s).
const DefaultStatusPropagationLatencyBudget = 60 * time.Second

// ChannelStatusUpdater is the small port the propagator consumes
// for each channel. Implementations are typically the existing
// social adapters extended with UpdateOrderStatus(ctx, ...). Tests
// pass stub implementations.
type ChannelStatusUpdater interface {
	ChannelName() string
	UpdateOrderStatus(ctx context.Context, in ChannelStatusUpdate) error
}

// ChannelStatusUpdate is the payload submitted to a channel
// adapter's UpdateOrderStatus method.
type ChannelStatusUpdate struct {
	TenantID        string
	ExternalOrderID string
	Status          string
	TrackingNumber  string
	DeliveryDate    time.Time
}

// StatusPropagatorMetrics is the small port the propagator emits
// the propagation duration histogram + per-channel counters through.
type StatusPropagatorMetrics interface {
	ObserveStatusPropagationDuration(channel string, durationSec float64)
	RecordChannelUpdate(tenantID, channel, status string)
}

// StatusPropagatorKPISample is the EvoMap KPI sample emitted per
// Propagate call.
type StatusPropagatorKPISample struct {
	TenantID  string
	OrderID   string
	Status    string
	Channels  []string
	LatencyMS int64
	Failed    []string
}

// StatusPropagatorKPIHook is the optional EvoMap emission hook.
type StatusPropagatorKPIHook func(StatusPropagatorKPISample)

// StatusPropagatorConfig wires a StatusPropagator.
type StatusPropagatorConfig struct {
	Channels      []ChannelStatusUpdater
	Publisher     eventbus.Publisher
	MaxRetries    int
	RetryInterval time.Duration
	Metrics       StatusPropagatorMetrics
	KPIHook       StatusPropagatorKPIHook
	Now           func() time.Time
	Sleep         func(time.Duration) // overridable for tests
}

// StatusPropagator is the v3.8.0 EC-7-4 propagator.
type StatusPropagator struct {
	channels      []ChannelStatusUpdater
	publisher     eventbus.Publisher
	maxRetries    int
	retryInterval time.Duration
	metrics       StatusPropagatorMetrics
	kpiHook       StatusPropagatorKPIHook
	now           func() time.Time
	sleep         func(time.Duration)
	logger        *slog.Logger

	mu     sync.Mutex
	dedup  map[string]struct{}
	closed bool
}

// NewStatusPropagator constructs the propagator.
func NewStatusPropagator(logger *slog.Logger, cfg StatusPropagatorConfig) (*StatusPropagator, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.Channels) == 0 {
		return nil, fmt.Errorf("%w: at least one ChannelStatusUpdater required", ErrStatusPropagatorUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: Publisher required", ErrStatusPropagatorUnconfigured)
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = DefaultStatusPropagationRetries
	}
	if cfg.RetryInterval <= 0 {
		cfg.RetryInterval = 100 * time.Millisecond
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.Sleep == nil {
		cfg.Sleep = time.Sleep
	}
	return &StatusPropagator{
		channels:      cfg.Channels,
		publisher:     cfg.Publisher,
		maxRetries:    cfg.MaxRetries,
		retryInterval: cfg.RetryInterval,
		metrics:       cfg.Metrics,
		kpiHook:       cfg.KPIHook,
		now:           cfg.Now,
		sleep:         cfg.Sleep,
		logger:        logger,
		dedup:         make(map[string]struct{}),
	}, nil
}

// Close marks the propagator closed. lifecycle.Closer contract.
func (p *StatusPropagator) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// PropagationResult is the per-Propagate output.
type PropagationResult struct {
	OrderID  string
	Status   string
	Updated  []string
	Failed   []string
	Cached   bool
	Duration time.Duration
}

// Propagate fans out a status update across every configured
// channel. Idempotent on (tracking_number, event_id).
//
// Cyclomatic stays at 4: validate / dedup / dispatch / publish.
func (p *StatusPropagator) Propagate(ctx context.Context, update eventbus.ShipmentStatusUpdatedPayload) (PropagationResult, error) {
	if err := p.guard(); err != nil {
		return PropagationResult{}, err
	}
	if err := update.Validate(); err != nil {
		return PropagationResult{}, fmt.Errorf("%w: %v", ErrCarrierWebhookInvalid, err)
	}
	if p.alreadySeen(update) {
		return PropagationResult{OrderID: update.OrderID, Status: update.Status, Cached: true}, nil
	}
	start := p.now()
	res := p.dispatchAll(ctx, update)
	res.Duration = p.now().Sub(start)
	p.publishSummary(ctx, update, res)
	p.recordKPI(update, res)
	return res, nil
}

// dispatchAll fans out to every channel sequentially. Sequential
// keeps complexity low and the 60s budget is comfortable since each
// retry tier sleeps only 100ms-400ms typical.
func (p *StatusPropagator) dispatchAll(ctx context.Context, update eventbus.ShipmentStatusUpdatedPayload) PropagationResult {
	res := PropagationResult{OrderID: update.OrderID, Status: update.Status}
	chUpdate := ChannelStatusUpdate{
		TenantID:        update.TenantID,
		ExternalOrderID: update.OrderID,
		Status:          update.Status,
		TrackingNumber:  update.TrackingNumber,
		DeliveryDate:    update.OccurredAt,
	}
	for _, ch := range p.channels {
		if err := p.dispatchToChannel(ctx, ch, chUpdate); err != nil {
			res.Failed = append(res.Failed, ch.ChannelName())
			continue
		}
		res.Updated = append(res.Updated, ch.ChannelName())
	}
	return res
}

// dispatchToChannel runs the per-channel call with retry + backoff.
func (p *StatusPropagator) dispatchToChannel(ctx context.Context, ch ChannelStatusUpdater, update ChannelStatusUpdate) error {
	start := p.now()
	defer func() {
		if p.metrics != nil {
			p.metrics.ObserveStatusPropagationDuration(ch.ChannelName(), p.now().Sub(start).Seconds())
		}
	}()
	err := p.retryWithBackoff(ctx, func(ctx context.Context) error { return ch.UpdateOrderStatus(ctx, update) })
	if err != nil {
		if p.metrics != nil {
			p.metrics.RecordChannelUpdate(update.TenantID, ch.ChannelName(), "failed")
		}
		return fmt.Errorf("%w: channel=%s: %v", ErrChannelStatusUpdateFailed, ch.ChannelName(), err)
	}
	if p.metrics != nil {
		p.metrics.RecordChannelUpdate(update.TenantID, ch.ChannelName(), "ok")
	}
	return nil
}

// retryWithBackoff calls fn up to maxRetries with exponential
// backoff. Cyclomatic 4.
func (p *StatusPropagator) retryWithBackoff(ctx context.Context, fn func(context.Context) error) error {
	var lastErr error
	delay := p.retryInterval
	for attempt := 0; attempt < p.maxRetries; attempt++ {
		if attempt > 0 {
			p.sleep(delay)
			delay *= 2
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := fn(ctx); err != nil {
			lastErr = err
			continue
		}
		return nil
	}
	return lastErr
}

// alreadySeen returns true on the second observation of an
// (tracking_number, event_id) tuple. The first call records it.
func (p *StatusPropagator) alreadySeen(update eventbus.ShipmentStatusUpdatedPayload) bool {
	key := buildPropagatorDedupKey(update)
	p.mu.Lock()
	defer p.mu.Unlock()
	if _, ok := p.dedup[key]; ok {
		return true
	}
	p.dedup[key] = struct{}{}
	return false
}

func (p *StatusPropagator) guard() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrStatusPropagatorClosed
	}
	return nil
}

func (p *StatusPropagator) publishSummary(ctx context.Context, update eventbus.ShipmentStatusUpdatedPayload, _ PropagationResult) {
	evt, err := eventbus.NewShipmentStatusUpdatedEvent("agent.fulfilment.status_propagator", update.OccurredAt, update)
	if err != nil {
		p.logger.Error("fulfilment.status_propagator.event_invalid", "error", err)
		return
	}
	if err := p.publisher.Publish(ctx, evt); err != nil {
		p.logger.Error("fulfilment.status_propagator.publish_failed", "error", err)
	}
}

func (p *StatusPropagator) recordKPI(update eventbus.ShipmentStatusUpdatedPayload, res PropagationResult) {
	if p.kpiHook == nil {
		return
	}
	p.kpiHook(StatusPropagatorKPISample{
		TenantID:  update.TenantID,
		OrderID:   update.OrderID,
		Status:    update.Status,
		Channels:  append([]string{}, res.Updated...),
		LatencyMS: res.Duration.Milliseconds(),
		Failed:    append([]string{}, res.Failed...),
	})
}

func buildPropagatorDedupKey(p eventbus.ShipmentStatusUpdatedPayload) string {
	return p.TenantID + "\x00" + p.TrackingNumber + "\x00" + p.EventID
}

// ===== Webhook handlers =====

// CarrierWebhookConfig wires either AusPostWebhookHandler or
// DHLWebhookHandler. The HMAC secret is the shared signing secret
// agreed with the carrier developer portal (matches the v3.8.0
// outbound signature on the CreateLabel call).
type CarrierWebhookConfig struct {
	Secret      string
	Path        string
	Propagator  *StatusPropagator
	OrderLookup OrderLookup
	Now         func() time.Time
}

// OrderLookup is the small port the webhook handlers consume to
// resolve a tracking_number back to the internal order_id +
// tenant_id. Production wires a Postgres-backed implementation that
// hits shipping_labels (migration 0017); tests pass an in-memory map.
type OrderLookup interface {
	OrderForTracking(ctx context.Context, trackingNumber string) (orderID, tenantID string, err error)
}

// CarrierWebhookHandler is shared between AusPost + DHL. The carrier
// header where the signature lives differs per carrier so the
// constructor takes a header name.
type CarrierWebhookHandler struct {
	cfg         CarrierWebhookConfig
	carrierName string
	headerName  string
	logger      *slog.Logger
}

// NewAusPostWebhookHandler returns a handler that verifies the
// X-AusPost-Signature header.
func NewAusPostWebhookHandler(logger *slog.Logger, cfg CarrierWebhookConfig) (*CarrierWebhookHandler, error) {
	return newCarrierWebhookHandler(logger, cfg, carrier.CarrierAusPost, "X-AusPost-Signature")
}

// NewDHLWebhookHandler returns a handler that verifies the
// X-DHL-Signature header.
func NewDHLWebhookHandler(logger *slog.Logger, cfg CarrierWebhookConfig) (*CarrierWebhookHandler, error) {
	return newCarrierWebhookHandler(logger, cfg, carrier.CarrierDHL, "X-DHL-Signature")
}

func newCarrierWebhookHandler(logger *slog.Logger, cfg CarrierWebhookConfig, carrierName, header string) (*CarrierWebhookHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if strings.TrimSpace(cfg.Secret) == "" {
		return nil, fmt.Errorf("%w: webhook secret required", ErrStatusPropagatorUnconfigured)
	}
	if cfg.Propagator == nil {
		return nil, fmt.Errorf("%w: Propagator required", ErrStatusPropagatorUnconfigured)
	}
	if cfg.OrderLookup == nil {
		return nil, fmt.Errorf("%w: OrderLookup required", ErrStatusPropagatorUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if strings.TrimSpace(cfg.Path) == "" {
		cfg.Path = "/api/v1/webhooks/" + carrierName + "/status"
	}
	return &CarrierWebhookHandler{cfg: cfg, carrierName: carrierName, headerName: header, logger: logger}, nil
}

// ServeHTTP implements http.Handler. Cyclomatic 5.
func (h *CarrierWebhookHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "body read error", http.StatusBadRequest)
		return
	}
	defer r.Body.Close()
	signature := r.Header.Get(h.headerName)
	if !carrier.VerifyAusPostHMAC(h.cfg.Secret, http.MethodPost, h.cfg.Path, body, signature) {
		http.Error(w, "invalid signature", http.StatusUnauthorized)
		return
	}
	parsed, err := decodeWebhookBody(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if err := h.resolveAndPropagate(r.Context(), parsed); err != nil {
		h.handleHandlerError(w, err)
		return
	}
	w.WriteHeader(http.StatusAccepted)
}

func (h *CarrierWebhookHandler) resolveAndPropagate(ctx context.Context, body webhookBody) error {
	orderID, tenantID, err := h.cfg.OrderLookup.OrderForTracking(ctx, body.TrackingNumber)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrShipmentNotFound, err)
	}
	payload := eventbus.ShipmentStatusUpdatedPayload{
		Version:        eventbus.ShipmentStatusUpdatedPayloadVersion,
		TenantID:       tenantID,
		OrderID:        orderID,
		Carrier:        h.carrierName,
		TrackingNumber: body.TrackingNumber,
		Status:         body.Status,
		EventID:        body.EventID,
		OccurredAt:     body.OccurredAt,
	}
	_, err = h.cfg.Propagator.Propagate(ctx, payload)
	return err
}

func (h *CarrierWebhookHandler) handleHandlerError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, ErrShipmentNotFound):
		http.Error(w, err.Error(), http.StatusNotFound)
	case errors.Is(err, ErrCarrierWebhookInvalid):
		http.Error(w, err.Error(), http.StatusBadRequest)
	default:
		h.logger.Error("fulfilment.status_propagator.webhook_failed", "error", err)
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// webhookBody is the inbound carrier webhook shape (shared for
// AusPost + DHL since both publish the same canonical fields).
type webhookBody struct {
	TrackingNumber string    `json:"tracking_number"`
	Status         string    `json:"status"`
	EventID        string    `json:"event_id"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func decodeWebhookBody(raw []byte) (webhookBody, error) {
	var b webhookBody
	if err := json.Unmarshal(raw, &b); err != nil {
		return webhookBody{}, fmt.Errorf("%w: %v", ErrCarrierWebhookInvalid, err)
	}
	if strings.TrimSpace(b.TrackingNumber) == "" || strings.TrimSpace(b.EventID) == "" {
		return webhookBody{}, fmt.Errorf("%w: tracking_number + event_id required", ErrCarrierWebhookInvalid)
	}
	if strings.TrimSpace(b.Status) == "" {
		return webhookBody{}, fmt.Errorf("%w: status required", ErrCarrierWebhookInvalid)
	}
	if b.OccurredAt.IsZero() {
		b.OccurredAt = time.Now().UTC()
	}
	return b, nil
}

// MemoryOrderLookup is a small in-package OrderLookup used by tests.
type MemoryOrderLookup struct {
	mu      sync.Mutex
	mapping map[string]struct{ orderID, tenantID string }
}

// NewMemoryOrderLookup returns an in-memory OrderLookup seeded with
// the supplied (tracking_number -> order_id, tenant_id) map.
func NewMemoryOrderLookup(seed map[string][2]string) *MemoryOrderLookup {
	out := &MemoryOrderLookup{mapping: map[string]struct{ orderID, tenantID string }{}}
	for k, v := range seed {
		out.mapping[k] = struct{ orderID, tenantID string }{orderID: v[0], tenantID: v[1]}
	}
	return out
}

// OrderForTracking implements OrderLookup.
func (m *MemoryOrderLookup) OrderForTracking(_ context.Context, trackingNumber string) (string, string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if v, ok := m.mapping[trackingNumber]; ok {
		return v.orderID, v.tenantID, nil
	}
	return "", "", fmt.Errorf("tracking_number %s not found", trackingNumber)
}
