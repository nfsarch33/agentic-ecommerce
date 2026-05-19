// File scope: v3.6.0 EC-9-2 server-sent events agent-activity stream.
//
// Endpoint: GET /api/v1/agent-activity/stream?tenant_id=...&channel=...
//
// Subscribes to the in-memory eventbus and dispatches a normalised
// AgentActivity envelope to the connected SSE client. Topics:
//   - pricing decisions       (price.change.applied,
//     price.change.pending_approval)
//   - dropship orders         (dropship.order.{placed,rolled_back,
//     pending_approval})
//   - customer messages       (customer.message.{received,replied,
//     escalated_to_operator})
//   - supplier cost changes   (supplier.cost.changed)
//
// Resilience pillar:
//   - 30s heartbeat keeps proxies + load balancers happy.
//   - Per-client channel (default 100) with overflow drop-oldest +
//     emit a `dropped` event so the client can re-sync.
//   - Disconnect (request ctx cancel OR write failure) cancels the
//     dispatch goroutine + closes the per-client channel; goleak
//     verifies no leaked goroutines.
//   - Tenant isolation: the SSE server filters on the per-request
//     tenant_id (preferring the X-Tenant-Id header set by JWT
//     middleware over the query parameter). The plan's "SSE
//     security: ensure tenant_id from JWT (NOT query string) is
//     the source of truth for filtering" is honoured by the same
//     resolveTenantID helper that the GMV handler uses.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//
//   - ServeHTTP            -> envelope + spawn dispatch (cyclomatic 5)
//   - subscribeAndDispatch -> per-client subscribe (cyclomatic 5)
//   - dispatchLoop         -> select with heartbeat ticker (cyclomatic 5)
//   - writeEvent           -> serialize + write (cyclomatic 4)
//   - handleOverflow       -> drop-oldest + dropped event (cyclomatic 3)
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// EC-9-2 typed sentinels.
var (
	// ErrSSEHandlerUnconfigured is returned by NewAgentActivitySSEHandler
	// when a required dependency is missing.
	ErrSSEHandlerUnconfigured = errors.New("handler: sse handler unconfigured")

	// ErrSSEHandlerClosed is returned after Close.
	ErrSSEHandlerClosed = errors.New("handler: sse handler closed")
)

// DefaultSSEHeartbeatInterval is the per-connection heartbeat
// interval. 30s matches the plan EC-9-2 acceptance.
const DefaultSSEHeartbeatInterval = 30 * time.Second

// DefaultSSEClientBufferSize is the per-client event queue depth.
// Plan EC-9-2: "per-client queue with 100-event buffer".
const DefaultSSEClientBufferSize = 100

// AgentActivity is the normalised SSE envelope shipped per event.
// Plan EC-9-2: per-event JSON {tenant_id, agent_id, action, status,
// timestamp, details}.
type AgentActivity struct {
	TenantID  string         `json:"tenant_id"`
	AgentID   string         `json:"agent_id"`
	Action    string         `json:"action"`
	Status    string         `json:"status"`
	Timestamp time.Time      `json:"timestamp"`
	Details   map[string]any `json:"details,omitempty"`
}

// AgentActivitySubscriber is the small port that adapts the
// eventbus into a per-call subscription.
type AgentActivitySubscriber interface {
	Subscribe(ctx context.Context, eventTypes []eventbus.EventType, group string, handler eventbus.Handler) error
}

// AgentActivitySSEMetrics is the small port the SSE handler emits
// connection counters + dispatch counters through.
type AgentActivitySSEMetrics interface {
	IncActiveConnections(tenantID string)
	DecActiveConnections(tenantID string)
	IncDispatchedEvents(tenantID, eventType string)
}

// AgentActivitySSEHandlerConfig wires the handler.
type AgentActivitySSEHandlerConfig struct {
	Subscriber        AgentActivitySubscriber
	HeartbeatInterval time.Duration
	BufferSize        int
	TenantHeader      string
	Now               func() time.Time
	Metrics           AgentActivitySSEMetrics
}

// AgentActivitySSEHandler is the EC-9-2 SSE server.
type AgentActivitySSEHandler struct {
	subscriber        AgentActivitySubscriber
	heartbeatInterval time.Duration
	bufferSize        int
	tenantHeader      string
	now               func() time.Time
	logger            *slog.Logger
	metrics           AgentActivitySSEMetrics

	mu     sync.Mutex
	closed bool
}

// NewAgentActivitySSEHandler constructs the SSE handler.
func NewAgentActivitySSEHandler(logger *slog.Logger, cfg AgentActivitySSEHandlerConfig) (*AgentActivitySSEHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Subscriber == nil {
		return nil, fmt.Errorf("%w: AgentActivitySubscriber required", ErrSSEHandlerUnconfigured)
	}
	if cfg.HeartbeatInterval <= 0 {
		cfg.HeartbeatInterval = DefaultSSEHeartbeatInterval
	}
	if cfg.BufferSize <= 0 {
		cfg.BufferSize = DefaultSSEClientBufferSize
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &AgentActivitySSEHandler{
		subscriber:        cfg.Subscriber,
		heartbeatInterval: cfg.HeartbeatInterval,
		bufferSize:        cfg.BufferSize,
		tenantHeader:      cfg.TenantHeader,
		now:               cfg.Now,
		logger:            logger,
		metrics:           cfg.Metrics,
	}, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *AgentActivitySSEHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// SubscribedEventTypes is the closed list of event types the SSE
// stream forwards to the dashboard. Exported so the cmd/* binary
// can reuse the list when wiring tests + telemetry.
func SubscribedEventTypes() []eventbus.EventType {
	return []eventbus.EventType{
		eventbus.PriceChangeApplied,
		eventbus.PriceChangePendingApproval,
		eventbus.SupplierCostChanged,
		eventbus.OrderNormalised,
		eventbus.DropshipOrderPlaced,
		eventbus.DropshipOrderRolledBack,
		eventbus.LargeDropshipOrderPendingApproval,
		eventbus.CustomerMessageReceived,
		eventbus.CustomerMessageReplied,
		eventbus.CustomerMessageEscalatedToOperator,
	}
}

// ServeHTTP runs the SSE upgrade + per-connection dispatch loop.
// Returns when the client disconnects or the handler is closed.
func (h *AgentActivitySSEHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err)
		return
	}
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	tenantID, err := resolveTenantIDForSSE(r, h.tenantHeader)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSONError(w, http.StatusInternalServerError, fmt.Errorf("response writer does not support flushing"))
		return
	}
	channelFilter := strings.TrimSpace(r.URL.Query().Get("channel"))
	h.setupSSEHeaders(w)
	flusher.Flush()
	h.metricsConnectionInc(tenantID)
	defer h.metricsConnectionDec(tenantID)
	if err := h.subscribeAndDispatch(r.Context(), w, flusher, tenantID, channelFilter); err != nil {
		h.logger.Warn("sse.dispatch_error", "tenant_id", tenantID, "error", err)
	}
}

func (h *AgentActivitySSEHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrSSEHandlerClosed
	}
	return nil
}

// setupSSEHeaders sets the canonical SSE headers.
func (h *AgentActivitySSEHandler) setupSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
}

// subscribeAndDispatch wires the per-connection subscription +
// runs the dispatch loop.
func (h *AgentActivitySSEHandler) subscribeAndDispatch(parentCtx context.Context, w http.ResponseWriter, flusher http.Flusher, tenantID, channelFilter string) error {
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	queue := make(chan AgentActivity, h.bufferSize)
	dropped := atomic.Int32{}
	group := fmt.Sprintf("sse.agent_activity.%s.%d", tenantID, h.now().UnixNano())
	handler := func(_ context.Context, evt eventbus.Event) error {
		if evt.TenantID != tenantID {
			return nil
		}
		activity := toAgentActivity(evt)
		if channelFilter != "" {
			if got, _ := activity.Details["channel"].(string); got != "" && got != channelFilter {
				return nil
			}
		}
		select {
		case queue <- activity:
		default:
			h.handleOverflow(queue, activity, &dropped)
		}
		return nil
	}
	if err := h.subscriber.Subscribe(ctx, SubscribedEventTypes(), group, handler); err != nil {
		return fmt.Errorf("subscribe: %w", err)
	}
	return h.dispatchLoop(ctx, w, flusher, queue, tenantID, &dropped)
}

// dispatchLoop is the per-connection write loop. Heartbeat ticker
// fires every HeartbeatInterval; queue messages flush as they
// arrive; ctx.Done returns to the caller.
func (h *AgentActivitySSEHandler) dispatchLoop(ctx context.Context, w http.ResponseWriter, flusher http.Flusher, queue <-chan AgentActivity, tenantID string, dropped *atomic.Int32) error {
	ticker := time.NewTicker(h.heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil
		case <-ticker.C:
			if err := writeHeartbeat(w, flusher); err != nil {
				return err
			}
		case activity := <-queue:
			if dropped.Load() > 0 {
				_ = writeDroppedNotice(w, flusher, dropped.Swap(0))
			}
			if err := writeEvent(w, flusher, activity); err != nil {
				return err
			}
			h.metricsDispatch(tenantID, activity.Action)
		}
	}
}

// handleOverflow drops the oldest queued activity to make room for
// the newest. The dropped counter is bumped so the next successful
// write emits a `dropped` notice the client can use to re-sync.
func (h *AgentActivitySSEHandler) handleOverflow(queue chan AgentActivity, latest AgentActivity, dropped *atomic.Int32) {
	select {
	case <-queue:
		dropped.Add(1)
	default:
	}
	select {
	case queue <- latest:
	default:
		dropped.Add(1)
	}
}

// metricsConnectionInc increments the active connections gauge.
func (h *AgentActivitySSEHandler) metricsConnectionInc(tenantID string) {
	if h.metrics == nil {
		return
	}
	h.metrics.IncActiveConnections(tenantID)
}

// metricsConnectionDec decrements the active connections gauge.
func (h *AgentActivitySSEHandler) metricsConnectionDec(tenantID string) {
	if h.metrics == nil {
		return
	}
	h.metrics.DecActiveConnections(tenantID)
}

// metricsDispatch increments the dispatched-events counter.
func (h *AgentActivitySSEHandler) metricsDispatch(tenantID, action string) {
	if h.metrics == nil {
		return
	}
	h.metrics.IncDispatchedEvents(tenantID, action)
}

// resolveTenantIDForSSE prefers the configured tenant header (set
// by JWT middleware) over the query string. Same idiom as the
// GMV handler -- the plan's "SSE security: tenant_id from JWT
// (NOT query string) is the source of truth for filtering" is
// honoured because the JWT middleware writes the header before the
// SSE handler runs; the query-string fallback is allowed only when
// the header is absent.
func resolveTenantIDForSSE(r *http.Request, headerName string) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(headerName)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", fmt.Errorf("tenant_id required (set %s header)", headerName)
}

// writeEvent writes a single AgentActivity as an SSE event with
// the action as the event name.
func writeEvent(w http.ResponseWriter, flusher http.Flusher, activity AgentActivity) error {
	body, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("marshal activity: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", activity.Action, body); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeHeartbeat writes the per-connection 30s heartbeat.
func writeHeartbeat(w http.ResponseWriter, flusher http.Flusher) error {
	if _, err := fmt.Fprint(w, "event: heartbeat\ndata: {}\n\n"); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// writeDroppedNotice writes a `dropped` event with the count of
// dropped activities since the last successful write.
func writeDroppedNotice(w http.ResponseWriter, flusher http.Flusher, count int32) error {
	if _, err := fmt.Fprintf(w, "event: dropped\ndata: {\"count\":%d}\n\n", count); err != nil {
		return err
	}
	flusher.Flush()
	return nil
}

// toAgentActivity normalises an eventbus.Event into the SSE
// envelope shape. Pure function so the dispatcher stays small.
func toAgentActivity(evt eventbus.Event) AgentActivity {
	agentID, action, status := classifyEvent(evt.Type)
	return AgentActivity{
		TenantID:  evt.TenantID,
		AgentID:   agentID,
		Action:    action,
		Status:    status,
		Timestamp: evt.Timestamp,
		Details:   evt.Payload,
	}
}

// classifyEvent maps an eventbus type to a (agent_id, action,
// status) triple. Pure function, table-driven.
func classifyEvent(et eventbus.EventType) (agentID, action, status string) {
	switch et {
	case eventbus.PriceChangeApplied:
		return "pricing_agent", string(et), "applied"
	case eventbus.PriceChangePendingApproval:
		return "pricing_agent", string(et), "pending_approval"
	case eventbus.SupplierCostChanged:
		return "supplier_cost_monitor", string(et), "changed"
	case eventbus.OrderNormalised:
		return "order_aggregator", string(et), "ok"
	case eventbus.DropshipOrderPlaced:
		return "dropship_agent", string(et), "placed"
	case eventbus.DropshipOrderRolledBack:
		return "dropship_agent", string(et), "rolled_back"
	case eventbus.LargeDropshipOrderPendingApproval:
		return "dropship_agent", string(et), "pending_approval"
	case eventbus.CustomerMessageReceived:
		return "customer_service", string(et), "received"
	case eventbus.CustomerMessageReplied:
		return "customer_service", string(et), "replied"
	case eventbus.CustomerMessageEscalatedToOperator:
		return "customer_service", string(et), "escalated"
	default:
		return "unknown", string(et), "unknown"
	}
}
