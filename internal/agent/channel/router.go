// File scope: v3.4.0 EC-4-3 cross-platform channel router.
//
// The router subscribes to ProductEnrichedEvent (mirroring the
// v3.3.0 EC-3-2 TikTokListingAgent) but unlike the single-channel
// listing agent it fans the same event out to one or more channel
// adapters in parallel. Each adapter publishes to a different
// storefront (TikTok Shop, Facebook Shop, RedNote uiauto bridge,
// Instagram, etc).
//
// Routing decision: each channel descriptor carries a ChannelMatcher
// closure that inspects the typed payload (categories, source,
// quality score, optional Tagger output) and returns true when the
// channel should publish the event. Operator-configured channel
// weights are deliberately out-of-scope for v3.4.0 MVP -- the
// initial behaviour is "fan-out to ALL matching channels". Future
// AB-test / canary release can layer weight on top by composing
// matchers.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): the router splits into HandleEvent (envelope gate),
// dispatchAll (matcher loop + workerpool fan-out), dispatchOne
// (per-channel publish + DLQ + metric), and decodeRouterEnriched
// (payload extraction). Per-function cyclomatic stays under 6.
//
// Resilience pillar:
//   - Implements lifecycle.Closer.
//   - Uses workerpool.Pool for goroutine fan-out (NEVER raw `go func()`).
//   - Adapter failures route to a DLQ port (in-memory default;
//     persistent backends layer behind the same interface).
//   - Prometheus metrics emitted via RouterMetrics.
//   - Tenant awareness: every dispatch is gated on the typed
//     ProductEnrichedPayload TenantID; metric labels carry it.
//
// Cite skill: go-clean-architecture (port + adapter; the router
// depends on the ChannelAdapter port, not on a concrete
// TikTokShopClient / FacebookShopClient).
package channel

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/channelport"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// ChannelAdapter is the port every storefront channel implements.
// The TikTok adapter wraps social.Client; the Facebook adapter
// wraps social.FacebookClient; the RedNote adapter wraps the
// omniparser-bridge facade. Tests use a fake.
type ChannelAdapter interface {
	// Name returns a stable label used for routing tables, metric
	// labels, and DLQ records. Examples: "tiktok", "facebook",
	// "rednote", "instagram".
	Name() string
	// Publish fan-outs the enriched product to the channel. Returns
	// nil on success or a typed error on failure. The router wraps
	// the error with the channel name + DLQ sentinel as needed.
	Publish(ctx context.Context, payload eventbus.ProductEnrichedPayload) error
	// Close releases any per-channel resources. Implements
	// lifecycle.Closer.
	Close(ctx context.Context) error
}

// ChannelMatcher decides whether a channel should publish a given
// payload. Pure function; the operator wires the production matchers
// at the composition root (e.g., "rednote when payload.CategoryID
// starts with 'lifestyle.'", "facebook when tags contain 'au-domestic'").
type ChannelMatcher func(payload eventbus.ProductEnrichedPayload) bool

// MatchAlways is the trivial matcher used by the operator-configured
// "fallback" channel that should receive every event.
func MatchAlways(_ eventbus.ProductEnrichedPayload) bool { return true }

// MatchNever is the trivial matcher used in tests + the disabled
// channel state.
func MatchNever(_ eventbus.ProductEnrichedPayload) bool { return false }

// ChannelDescriptor pairs a channel adapter with the matcher that
// gates publishing.
type ChannelDescriptor struct {
	Adapter ChannelAdapter
	Matcher ChannelMatcher
}

// ChannelDispatchResult is the per-channel outcome of a single
// router dispatch. The Cause field is one of:
//   - ErrChannelDelivered (success)
//   - wrapped(ErrChannelDLQ + adapter error) on failure that was
//     enqueued in the DLQ
//   - the raw adapter error when the DLQ write itself failed
type ChannelDispatchResult struct {
	Channel string
	Cause   error
	Outcome string // "delivered" | "dlq" | "no_match"
}

// DLQRecord captures the context the operator needs to retry or
// inspect a failed delivery. Persistent DLQ adapters serialise this
// shape directly.
type DLQRecord struct {
	TenantID   string
	ProductID  string
	Channel    string
	Reason     string
	OccurredAt time.Time
}

// DLQ is the small port for failed-delivery enqueueing. The default
// in-memory implementation is provided by InMemoryDLQ; persistent
// backends (Redis stream, Postgres outbox) implement the same shape.
type DLQ interface {
	Enqueue(ctx context.Context, rec DLQRecord) error
}

// RouterMetrics is the small port the router uses to emit
// Prometheus counters without coupling to internal/metrics.Registry.
type RouterMetrics interface {
	RecordDispatch(tenantID, channel, outcome string)
	RecordDLQ(tenantID, channel, reason string)
}

// ChannelRouterConfig wires a ChannelRouter.
type ChannelRouterConfig struct {
	TenantID          string
	Channels          []ChannelDescriptor
	Pool              *workerpool.Pool
	Publisher         eventbus.Publisher
	Consumer          eventbus.Consumer
	DLQ               DLQ
	Metrics           RouterMetrics
	Now               func() time.Time
	SubscriptionGroup string
	// DispatchTimeout caps a single channel publish call. Defaults
	// to 30 seconds. Each fan-out task derives ctx via
	// context.WithTimeout from the parent.
	DispatchTimeout time.Duration
}

// ChannelRouter is the v3.4.0 EC-4-3 router.
type ChannelRouter struct {
	cfg ChannelRouterConfig
	log *slog.Logger
	now func() time.Time

	mu     sync.Mutex
	closed bool
}

// NewChannelRouter constructs a router. Defaults: in-memory DLQ,
// 30s DispatchTimeout, "channel.router" subscription group, Now =
// time.Now.
//
// Decomposition: validation + defaults split into helpers so this
// constructor body keeps cyclomatic complexity well under the v3.1.0
// sentrux ceiling.
func NewChannelRouter(logger *slog.Logger, cfg ChannelRouterConfig) (*ChannelRouter, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if err := validateChannelRouterConfig(cfg); err != nil {
		return nil, err
	}
	applyChannelRouterDefaults(&cfg)
	return &ChannelRouter{cfg: cfg, log: logger, now: cfg.Now}, nil
}

// validateChannelRouterConfig enforces the required-dependency
// contract. Pure; no side effects.
func validateChannelRouterConfig(cfg ChannelRouterConfig) error {
	if strings.TrimSpace(cfg.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrRouterUnconfigured)
	}
	if cfg.Pool == nil {
		return fmt.Errorf("%w: workerpool.Pool required (no raw `go func()`)", ErrRouterUnconfigured)
	}
	if cfg.Publisher == nil {
		return fmt.Errorf("%w: eventbus.Publisher required", ErrRouterUnconfigured)
	}
	if len(cfg.Channels) == 0 {
		return fmt.Errorf("%w: at least one ChannelDescriptor required", ErrRouterUnconfigured)
	}
	return nil
}

// applyChannelRouterDefaults fills in optional fields. Mutates the
// supplied config in place so the constructor body stays tiny.
func applyChannelRouterDefaults(cfg *ChannelRouterConfig) {
	if cfg.DLQ == nil {
		cfg.DLQ = NewInMemoryDLQ()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.DispatchTimeout <= 0 {
		cfg.DispatchTimeout = 30 * time.Second
	}
	if cfg.SubscriptionGroup == "" {
		cfg.SubscriptionGroup = "channel.router"
	}
}

// Close marks the router closed. Implements lifecycle.Closer.
// Note: the underlying workerpool is owned by the composition root
// so we do NOT close it here.
func (r *ChannelRouter) Close(_ context.Context) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	return nil
}

// Start subscribes to ProductEnrichedEvent on the supplied
// Consumer. Returns the Subscribe error directly so the composition
// root can fail boot when the bus is down.
func (r *ChannelRouter) Start(ctx context.Context) error {
	if r.cfg.Consumer == nil {
		return fmt.Errorf("%w: Consumer required for Start", ErrRouterUnconfigured)
	}
	return r.cfg.Consumer.Subscribe(ctx, []eventbus.EventType{eventbus.ProductEnriched}, r.cfg.SubscriptionGroup, r.HandleEvent)
}

// HandleEvent is the eventbus dispatch entry point. Decodes the
// payload, gates on tenant scope, and delegates to dispatchAll.
func (r *ChannelRouter) HandleEvent(ctx context.Context, evt eventbus.Event) error {
	if err := r.guard(); err != nil {
		return err
	}
	payload, err := decodeRouterEnriched(evt)
	if err != nil {
		return err
	}
	if payload.TenantID != r.cfg.TenantID {
		return fmt.Errorf("%w: event=%s router=%s", ErrChannelTenantMismatch, payload.TenantID, r.cfg.TenantID)
	}
	return r.dispatchAll(ctx, payload)
}

// dispatchAll is the matcher loop + workerpool fan-out. Returns
// ErrNoMatchingChannel (wrapped) when no channel matched, wrapped
// ErrChannelDLQ when at least one matched channel failed and was
// DLQ-enqueued, or nil on full success.
func (r *ChannelRouter) dispatchAll(ctx context.Context, payload eventbus.ProductEnrichedPayload) error {
	matched := r.collectMatched(payload)
	if len(matched) == 0 {
		r.recordDispatch("(none)", "no_match")
		return fmt.Errorf("%w: tenant=%s product=%s", ErrNoMatchingChannel, payload.TenantID, payload.ProductID)
	}
	results := r.fanOut(ctx, matched, payload)
	failed := countFailed(results)
	if failed > 0 {
		return fmt.Errorf("%w: %d/%d channels failed (tenant=%s product=%s)", ErrChannelDLQ, failed, len(results), payload.TenantID, payload.ProductID)
	}
	return nil
}

// countFailed returns the number of dispatch results that did not
// resolve to a "delivered" or "not_yet_implemented" outcome. Pure;
// no allocations. Cyclomatic 3.
func countFailed(results []ChannelDispatchResult) int {
	failed := 0
	for _, res := range results {
		if errors.Is(res.Cause, ErrChannelDelivered) {
			continue
		}
		if errors.Is(res.Cause, ErrChannelNotYetImplemented) {
			continue
		}
		failed++
	}
	return failed
}

// collectMatched walks the configured channel descriptors and keeps
// the ones whose matcher returns true.
func (r *ChannelRouter) collectMatched(payload eventbus.ProductEnrichedPayload) []ChannelDescriptor {
	out := make([]ChannelDescriptor, 0, len(r.cfg.Channels))
	for _, ch := range r.cfg.Channels {
		if ch.Adapter == nil || ch.Matcher == nil {
			continue
		}
		if ch.Matcher(payload) {
			out = append(out, ch)
		}
	}
	return out
}

// fanOut submits each matched-channel publish call as a workerpool
// task and waits for all to complete. Returns one result per
// channel (order matches input).
func (r *ChannelRouter) fanOut(ctx context.Context, matched []ChannelDescriptor, payload eventbus.ProductEnrichedPayload) []ChannelDispatchResult {
	results := make([]ChannelDispatchResult, len(matched))
	var wg sync.WaitGroup
	for i, ch := range matched {
		i, ch := i, ch
		wg.Add(1)
		err := r.cfg.Pool.Submit(ctx, func(taskCtx context.Context) error {
			defer wg.Done()
			results[i] = r.dispatchOne(taskCtx, ch, payload)
			return nil
		})
		if err != nil {
			// Pool saturated or closed -- treat as a failed dispatch
			// for this channel; do not block the whole fan-out.
			results[i] = ChannelDispatchResult{Channel: ch.Adapter.Name(), Outcome: "dlq", Cause: fmt.Errorf("%w: pool submit: %v", ErrChannelDLQ, err)}
			r.enqueueDLQ(ctx, payload, ch.Adapter.Name(), err)
			r.recordDLQ(ch.Adapter.Name(), "pool_saturated")
			wg.Done()
		}
	}
	wg.Wait()
	return results
}

// dispatchOne publishes to a single channel, recording metrics and
// pushing to DLQ on failure. Always returns a populated result.
//
// v3.9.1 EC-4-4: stub channels (Instagram + Pinterest) surface
// ErrChannelNotImplemented today; the router recognises the typed
// sentinel and emits ChannelStatusNotYetImplemented as a
// non-failure outcome (no DLQ, no delivered metric -- a dedicated
// "not_yet_implemented" outcome label keeps dashboards honest).
// Decomposition: the stub-recognition path stays in a tiny helper
// (cyclomatic 2) so dispatchOne stays under cyclomatic 6.
func (r *ChannelRouter) dispatchOne(parent context.Context, ch ChannelDescriptor, payload eventbus.ProductEnrichedPayload) ChannelDispatchResult {
	ctx, cancel := context.WithTimeout(parent, r.cfg.DispatchTimeout)
	defer cancel()
	name := ch.Adapter.Name()
	err := ch.Adapter.Publish(ctx, payload)
	if err == nil {
		r.recordDispatch(name, "delivered")
		return ChannelDispatchResult{Channel: name, Outcome: "delivered", Cause: ErrChannelDelivered}
	}
	if r.handleStubNotImplemented(parent, payload, name, err) {
		return ChannelDispatchResult{Channel: name, Outcome: "not_yet_implemented", Cause: ErrChannelNotYetImplemented}
	}
	r.log.Warn("channel.router.publish_failed", "tenant_id", payload.TenantID, "product_id", payload.ProductID, "channel", name, "error", err)
	r.recordDispatch(name, "dlq")
	r.enqueueDLQ(parent, payload, name, err)
	r.recordDLQ(name, "publish_failed")
	return ChannelDispatchResult{Channel: name, Outcome: "dlq", Cause: fmt.Errorf("%w: channel=%s: %w", ErrChannelDLQ, name, err)}
}

// handleStubNotImplemented surfaces the v3.9.1 EC-4-4 stub-channel
// recognition. Returns true when the dispatch was treated as a stub
// outcome (no DLQ); false otherwise. Cyclomatic 2.
func (r *ChannelRouter) handleStubNotImplemented(parent context.Context, payload eventbus.ProductEnrichedPayload, name string, err error) bool {
	if !errors.Is(err, ErrChannelNotYetImplemented) && !channelport.IsStubChannel(name) {
		return false
	}
	r.recordDispatch(name, "not_yet_implemented")
	if r.cfg.Publisher == nil {
		return true
	}
	evt, evtErr := eventbus.NewChannelStatusNotYetImplementedEvent("channel.router", r.now(), eventbus.ChannelStatusNotYetImplementedPayload{
		Version:    eventbus.ChannelStatusNotYetImplementedPayloadVersion,
		TenantID:   payload.TenantID,
		Channel:    name,
		Op:         "publish",
		ProductID:  payload.ProductID,
		Reason:     err.Error(),
		OccurredAt: r.now(),
	})
	if evtErr != nil {
		r.log.Warn("channel.router.stub_event_failed", "tenant_id", payload.TenantID, "channel", name, "error", evtErr)
		return true
	}
	if pubErr := r.cfg.Publisher.Publish(parent, evt); pubErr != nil {
		r.log.Warn("channel.router.stub_publish_failed", "tenant_id", payload.TenantID, "channel", name, "error", pubErr)
	}
	return true
}

// enqueueDLQ writes a DLQRecord; failures are logged but do not
// surface as the primary error (the channel publish error stays
// the primary signal).
func (r *ChannelRouter) enqueueDLQ(ctx context.Context, payload eventbus.ProductEnrichedPayload, channelName string, cause error) {
	rec := DLQRecord{
		TenantID:   payload.TenantID,
		ProductID:  payload.ProductID,
		Channel:    channelName,
		Reason:     cause.Error(),
		OccurredAt: r.now().UTC(),
	}
	if err := r.cfg.DLQ.Enqueue(ctx, rec); err != nil {
		r.log.Error("channel.router.dlq_enqueue_failed", "tenant_id", payload.TenantID, "channel", channelName, "error", err)
	}
}

func (r *ChannelRouter) recordDispatch(channel, outcome string) {
	if r.cfg.Metrics == nil {
		return
	}
	r.cfg.Metrics.RecordDispatch(r.cfg.TenantID, channel, outcome)
}

func (r *ChannelRouter) recordDLQ(channel, reason string) {
	if r.cfg.Metrics == nil {
		return
	}
	r.cfg.Metrics.RecordDLQ(r.cfg.TenantID, channel, reason)
}

func (r *ChannelRouter) guard() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return ErrRouterClosed
	}
	return nil
}

// decodeRouterEnriched is the v3.4.0 router-side payload decode.
// Reuses the shared helpers in tiktok_listing.go via decodePayloadMap.
func decodeRouterEnriched(evt eventbus.Event) (eventbus.ProductEnrichedPayload, error) {
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

// --- in-memory DLQ ---------------------------------------------------------

// InMemoryDLQ is the v3.4.0 default DLQ -- a goroutine-safe ring of
// DLQRecord values. Persistent backends (Redis stream, Postgres
// outbox) implement the same DLQ port. Capacity defaults to 1024;
// when full the oldest record is dropped (and dropped++).
type InMemoryDLQ struct {
	mu       sync.Mutex
	records  []DLQRecord
	capacity int
	dropped  int
}

// NewInMemoryDLQ returns a DLQ with default capacity 1024.
func NewInMemoryDLQ() *InMemoryDLQ {
	return &InMemoryDLQ{capacity: 1024}
}

// NewInMemoryDLQWithCapacity returns a DLQ with the supplied
// capacity. Useful for tight tests.
func NewInMemoryDLQWithCapacity(capacity int) *InMemoryDLQ {
	if capacity <= 0 {
		capacity = 1024
	}
	return &InMemoryDLQ{capacity: capacity}
}

// Enqueue appends a record. Drops the oldest when over capacity.
func (q *InMemoryDLQ) Enqueue(_ context.Context, rec DLQRecord) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.records) >= q.capacity {
		q.records = q.records[1:]
		q.dropped++
	}
	q.records = append(q.records, rec)
	return nil
}

// Records returns a copy of the buffered records (test helper).
func (q *InMemoryDLQ) Records() []DLQRecord {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]DLQRecord, len(q.records))
	copy(out, q.records)
	return out
}

// Dropped reports the count of records evicted due to capacity.
func (q *InMemoryDLQ) Dropped() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.dropped
}
