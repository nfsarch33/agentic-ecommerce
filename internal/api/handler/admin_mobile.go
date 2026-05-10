// File scope: v4.8.0 Story 1 -- mobile-optimized admin API surface
// for the Flutter admin app.
//
// Endpoints (REST + JSON, all tenant-scoped via X-Tenant-Id header):
//
//   - GET  /api/v1/admin/summary       -> compact dashboard KPIs
//   - GET  /api/v1/admin/orders        -> paginated order list
//   - POST /api/v1/admin/alerts/{id}/action -> quick alert resolution
//   - GET  /api/v1/admin/channels      -> channel health summary
//
// Response envelope: {data, meta: {page, limit, total}, links: {next, prev}}
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - ServeHTTP           -> route + guard (cyclomatic 4)
//   - handleSummary       -> fetch + assemble (cyclomatic 3)
//   - handleOrders        -> parse pagination + repo (cyclomatic 3)
//   - handleAlertAction   -> parse id + action + repo (cyclomatic 4)
//   - handleChannels      -> repo + write (cyclomatic 2)
//   - parsePagination     -> query params (cyclomatic 3)
//   - buildLinks          -> next/prev URL construction (cyclomatic 3)
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

var (
	ErrAdminMobileUnconfigured = errors.New("handler: admin mobile unconfigured")
	ErrAdminMobileClosed       = errors.New("handler: admin mobile closed")
	ErrAdminTenantMissing      = errors.New("handler: admin tenant_id missing")
	ErrAdminAlertIDMissing     = errors.New("handler: admin alert_id missing")
	ErrAdminInvalidAction      = errors.New("handler: admin invalid alert action")
)

type AdminSummary struct {
	ActiveOrders  int64                `json:"active_orders"`
	GMVTodayCents int64                `json:"gmv_today_aud_cents"`
	PendingAlerts int                  `json:"pending_alerts"`
	ChannelHealth []AdminChannelStatus `json:"channel_health"`
}

type AdminChannelStatus struct {
	Channel string `json:"channel"`
	Status  string `json:"status"`
}

type AdminOrderRow struct {
	OrderID    string    `json:"order_id"`
	Status     string    `json:"status"`
	TotalCents int64     `json:"total_cents"`
	Channel    string    `json:"channel"`
	CreatedAt  time.Time `json:"created_at"`
}

type AdminAlertActionRequest struct {
	Action string `json:"action"` // approve, deny, snooze
}

type PaginationMeta struct {
	Page  int `json:"page"`
	Limit int `json:"limit"`
	Total int `json:"total"`
}

type PaginationLinks struct {
	Next string `json:"next,omitempty"`
	Prev string `json:"prev,omitempty"`
}

type AdminMobileRepository interface {
	AdminSummary(ctx context.Context, tenantID string) (AdminSummary, error)
	AdminOrders(ctx context.Context, tenantID string, page, limit int) ([]AdminOrderRow, int, error)
	AdminResolveAlert(ctx context.Context, tenantID, alertID, action string, at time.Time) error
	AdminChannels(ctx context.Context, tenantID string) ([]AdminChannelStatus, error)
}

type AdminMobileMetrics interface {
	ObserveAdminAPIDuration(endpoint string, durationSec float64)
}

type AdminMobileHandlerConfig struct {
	Repository   AdminMobileRepository
	TenantHeader string
	Now          func() time.Time
	Metrics      AdminMobileMetrics
}

type AdminMobileHandler struct {
	repo         AdminMobileRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      AdminMobileMetrics

	mu     sync.Mutex
	closed bool
}

func NewAdminMobileHandler(logger *slog.Logger, cfg AdminMobileHandlerConfig) (*AdminMobileHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: AdminMobileRepository required", ErrAdminMobileUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &AdminMobileHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
	}, nil
}

func (h *AdminMobileHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

func (h *AdminMobileHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/admin")
	suffix = strings.TrimSuffix(suffix, "/")
	start := h.now()
	switch {
	case suffix == "/summary" && r.Method == http.MethodGet:
		h.handleSummary(w, r)
		h.recordDur("summary", start)
	case suffix == "/orders" && r.Method == http.MethodGet:
		h.handleOrders(w, r)
		h.recordDur("orders", start)
	case strings.HasSuffix(suffix, "/action") && r.Method == http.MethodPost:
		h.handleAlertAction(w, r, suffix)
		h.recordDur("alert_action", start)
	case suffix == "/channels" && r.Method == http.MethodGet:
		h.handleChannels(w, r)
		h.recordDur("channels", start)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown admin route: %s %s", r.Method, r.URL.Path))
	}
}

func (h *AdminMobileHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrAdminMobileClosed
	}
	return nil
}

func (h *AdminMobileHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveAdminTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	summary, err := h.repo.AdminSummary(r.Context(), tenantID)
	if err != nil {
		h.logger.Error("admin.summary_failed", "tenant_id", tenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": summary})
}

func (h *AdminMobileHandler) handleOrders(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveAdminTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	page, limit := parsePagination(r)
	rows, total, err := h.repo.AdminOrders(r.Context(), tenantID, page, limit)
	if err != nil {
		h.logger.Error("admin.orders_failed", "tenant_id", tenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	links := buildLinks("/api/v1/admin/orders", page, limit, total)
	writeJSON(w, http.StatusOK, map[string]any{
		"data":  rows,
		"meta":  PaginationMeta{Page: page, Limit: limit, Total: total},
		"links": links,
	})
}

func (h *AdminMobileHandler) handleAlertAction(w http.ResponseWriter, r *http.Request, suffix string) {
	tenantID, err := h.resolveAdminTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	alertID, err := parseAdminAlertID(suffix)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	action := strings.TrimSpace(r.URL.Query().Get("action"))
	if !isValidAdminAlertAction(action) {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("%w: %q", ErrAdminInvalidAction, action))
		return
	}
	if err := h.repo.AdminResolveAlert(r.Context(), tenantID, alertID, action, h.now()); err != nil {
		h.logger.Error("admin.alert_action_failed", "tenant_id", tenantID, "alert_id", alertID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"data": map[string]string{"alert_id": alertID, "action": action, "status": "resolved"},
	})
}

func (h *AdminMobileHandler) handleChannels(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveAdminTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	channels, err := h.repo.AdminChannels(r.Context(), tenantID)
	if err != nil {
		h.logger.Error("admin.channels_failed", "tenant_id", tenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"data": channels})
}

func (h *AdminMobileHandler) resolveAdminTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrAdminTenantMissing
}

func (h *AdminMobileHandler) recordDur(endpoint string, start time.Time) {
	if h.metrics == nil {
		return
	}
	h.metrics.ObserveAdminAPIDuration(endpoint, h.now().Sub(start).Seconds())
}

func parsePagination(r *http.Request) (int, int) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if page < 1 {
		page = 1
	}
	if limit < 1 || limit > 100 {
		limit = 20
	}
	return page, limit
}

func buildLinks(basePath string, page, limit, total int) PaginationLinks {
	var links PaginationLinks
	if page*limit < total {
		links.Next = fmt.Sprintf("%s?page=%d&limit=%d", basePath, page+1, limit)
	}
	if page > 1 {
		links.Prev = fmt.Sprintf("%s?page=%d&limit=%d", basePath, page-1, limit)
	}
	return links
}

func parseAdminAlertID(suffix string) (string, error) {
	// suffix: /alerts/{id}/action
	trimmed := strings.TrimPrefix(suffix, "/alerts/")
	trimmed = strings.TrimSuffix(trimmed, "/action")
	trimmed = strings.TrimSpace(trimmed)
	if trimmed == "" || trimmed == suffix {
		return "", ErrAdminAlertIDMissing
	}
	return trimmed, nil
}

func isValidAdminAlertAction(action string) bool {
	switch action {
	case "approve", "deny", "snooze":
		return true
	default:
		return false
	}
}
