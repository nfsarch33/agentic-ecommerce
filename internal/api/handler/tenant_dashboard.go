// File scope: v4.7.0 Story 3 -- per-tenant real-time observability
// dashboard HTTP handler.
//
// Endpoint: GET /api/v1/tenants/{tenant_id}/dashboard
//
//	-> JSON KPIs: active_orders, gmv_today, gmv_mtd,
//	   pending_alerts, active_channels, channel_health_summary,
//	   recent_agent_actions (last 10).
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - ServeHTTP         -> route + guard (cyclomatic 4)
//   - handleDashboard   -> fetch + assemble (cyclomatic 3)
//   - fetchOrders       -> repo call (cyclomatic 2)
//   - fetchGMV          -> repo call (cyclomatic 2)
//   - fetchAlerts       -> repo call (cyclomatic 2)
//   - assembleResponse  -> merge data (cyclomatic 1)
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

var (
	ErrTenantDashboardUnconfigured = errors.New("handler: tenant dashboard unconfigured")
	ErrTenantDashboardClosed       = errors.New("handler: tenant dashboard closed")
	ErrDashboardTenantMissing      = errors.New("handler: dashboard tenant_id missing")
)

// TenantDashboardKPIs is the response envelope.
type TenantDashboardKPIs struct {
	TenantID             string               `json:"tenant_id"`
	ActiveOrders         int64                `json:"active_orders"`
	GMVTodayCents        int64                `json:"gmv_today_aud_cents"`
	GMVMTDCents          int64                `json:"gmv_mtd_aud_cents"`
	PendingAlerts        int                  `json:"pending_alerts"`
	ActiveChannels       int                  `json:"active_channels"`
	ChannelHealthSummary []ChannelHealthEntry `json:"channel_health_summary"`
	RecentAgentActions   []RecentAgentAction  `json:"recent_agent_actions"`
	GeneratedAt          time.Time            `json:"generated_at"`
}

// ChannelHealthEntry summarises one channel's health state.
type ChannelHealthEntry struct {
	Channel string `json:"channel"`
	Status  string `json:"status"`
}

// RecentAgentAction is one row of the last-10 agent actions.
type RecentAgentAction struct {
	AgentID   string    `json:"agent_id"`
	Action    string    `json:"action"`
	Status    string    `json:"status"`
	Timestamp time.Time `json:"timestamp"`
}

// TenantDashboardRepository is the port the handler reads through.
type TenantDashboardRepository interface {
	ActiveOrders(ctx context.Context, tenantID string) (int64, error)
	GMVToday(ctx context.Context, tenantID string) (int64, error)
	GMVMTD(ctx context.Context, tenantID string) (int64, error)
	PendingAlerts(ctx context.Context, tenantID string) (int, error)
	ActiveChannels(ctx context.Context, tenantID string) (int, error)
	ChannelHealth(ctx context.Context, tenantID string) ([]ChannelHealthEntry, error)
	RecentAgentActions(ctx context.Context, tenantID string, limit int) ([]RecentAgentAction, error)
}

// TenantDashboardMetrics is the metrics port.
type TenantDashboardMetrics interface {
	ObserveTenantDashboardDuration(durationSec float64)
}

// TenantDashboardHandlerConfig wires the handler.
type TenantDashboardHandlerConfig struct {
	Repository   TenantDashboardRepository
	TenantHeader string
	Now          func() time.Time
	Metrics      TenantDashboardMetrics
}

// TenantDashboardHandler is the v4.7.0 per-tenant dashboard.
type TenantDashboardHandler struct {
	repo         TenantDashboardRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      TenantDashboardMetrics

	mu     sync.Mutex
	closed bool
}

// NewTenantDashboardHandler constructs the handler.
func NewTenantDashboardHandler(logger *slog.Logger, cfg TenantDashboardHandlerConfig) (*TenantDashboardHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: TenantDashboardRepository required", ErrTenantDashboardUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TenantDashboardHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
	}, nil
}

// Close marks the handler closed.
func (h *TenantDashboardHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes the dashboard request.
func (h *TenantDashboardHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err)
		return
	}
	if r.Method != http.MethodGet {
		writeJSONError(w, http.StatusMethodNotAllowed, fmt.Errorf("method %s not allowed", r.Method))
		return
	}
	start := h.now()
	defer h.recordDuration(start)
	h.handleDashboard(w, r)
}

func (h *TenantDashboardHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrTenantDashboardClosed
	}
	return nil
}

func (h *TenantDashboardHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	kpis, err := h.fetchAllKPIs(r.Context(), tenantID)
	if err != nil {
		h.logger.Error("tenant_dashboard.fetch_failed", "tenant_id", tenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, kpis)
}

func (h *TenantDashboardHandler) fetchAllKPIs(ctx context.Context, tenantID string) (TenantDashboardKPIs, error) {
	orders, err := h.repo.ActiveOrders(ctx, tenantID)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("active_orders: %w", err)
	}
	gmvToday, err := h.repo.GMVToday(ctx, tenantID)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("gmv_today: %w", err)
	}
	gmvMTD, err := h.repo.GMVMTD(ctx, tenantID)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("gmv_mtd: %w", err)
	}
	alerts, err := h.repo.PendingAlerts(ctx, tenantID)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("pending_alerts: %w", err)
	}
	channels, err := h.repo.ActiveChannels(ctx, tenantID)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("active_channels: %w", err)
	}
	health, err := h.repo.ChannelHealth(ctx, tenantID)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("channel_health: %w", err)
	}
	actions, err := h.repo.RecentAgentActions(ctx, tenantID, 10)
	if err != nil {
		return TenantDashboardKPIs{}, fmt.Errorf("recent_agent_actions: %w", err)
	}
	return h.assembleResponse(tenantID, orders, gmvToday, gmvMTD, alerts, channels, health, actions), nil
}

func (h *TenantDashboardHandler) assembleResponse(tenantID string, orders, gmvToday, gmvMTD int64, alerts, channels int, health []ChannelHealthEntry, actions []RecentAgentAction) TenantDashboardKPIs {
	return TenantDashboardKPIs{
		TenantID:             tenantID,
		ActiveOrders:         orders,
		GMVTodayCents:        gmvToday,
		GMVMTDCents:          gmvMTD,
		PendingAlerts:        alerts,
		ActiveChannels:       channels,
		ChannelHealthSummary: health,
		RecentAgentActions:   actions,
		GeneratedAt:          h.now(),
	}
}

func (h *TenantDashboardHandler) resolveTenantID(r *http.Request) (string, error) {
	path := r.URL.Path
	prefix := "/api/v1/tenants/"
	if idx := strings.Index(path, prefix); idx >= 0 {
		rest := path[idx+len(prefix):]
		if slashIdx := strings.Index(rest, "/"); slashIdx > 0 {
			tid := strings.TrimSpace(rest[:slashIdx])
			if tid != "" {
				return tid, nil
			}
		}
	}
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrDashboardTenantMissing
}

func (h *TenantDashboardHandler) recordDuration(start time.Time) {
	if h.metrics == nil {
		return
	}
	h.metrics.ObserveTenantDashboardDuration(h.now().Sub(start).Seconds())
}
