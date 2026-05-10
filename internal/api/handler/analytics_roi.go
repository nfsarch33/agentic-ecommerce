// File scope: v3.8.0 EC-9-3 ROI analytics HTTP handler.
//
// Endpoints (REST + JSON, all tenant-scoped):
//
//   - GET /api/v1/analytics/roi/heatmap?tenant_id=...&period=30d
//     -> 2-D matrix of ROI % across (channel, product).
//   - GET /api/v1/analytics/roi/dead-stock?tenant_id=...&min_age_days=60
//     -> filter for slow-moving inventory.
//   - GET /api/v1/analytics/roi/by-channel?from=ISO&to=ISO&tenant_id=...
//     -> ROI breakdown by channel.
//
// Reads the v3.8.0 roi_daily_rollup materialized view (migration
// 0019) via the small ROIRepository port. Tests use an in-memory
// implementation; production wires a Postgres adapter.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 13-sprint streak; v3.8.0 sprint 14 target):
//
//   - ServeHTTP            -> route by suffix (cyclomatic 4)
//   - parseROIFilter       -> validate query (cyclomatic 5)
//   - handleHeatmap        -> repo + write (cyclomatic 3)
//   - handleDeadStock      -> repo + filter + write (cyclomatic 3)
//   - handleByChannel      -> repo + write (cyclomatic 3)
//
// Each helper stays under cyclomatic 6.
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

// EC-9-3 typed sentinels.
var (
	// ErrROIHandlerUnconfigured is returned by NewROIHandler when a
	// required dependency is missing.
	ErrROIHandlerUnconfigured = errors.New("handler: roi handler unconfigured")

	// ErrROIHandlerClosed is returned after Close.
	ErrROIHandlerClosed = errors.New("handler: roi handler closed")

	// ErrInvalidPeriod is returned when the period query parameter is
	// missing, malformed, or out of bounds.
	ErrInvalidPeriod = errors.New("handler: roi invalid period")

	// ErrInsufficientData is returned when the rollup contains too
	// few rows to produce a meaningful heatmap.
	ErrInsufficientData = errors.New("handler: roi insufficient data")

	// ErrTenantHasNoOrders is returned when the tenant has zero
	// rollup rows in the requested window.
	ErrTenantHasNoOrders = errors.New("handler: tenant has no orders")

	// ErrROITenantMissing is returned when the tenant_id cannot be
	// derived from the request.
	ErrROITenantMissing = errors.New("handler: roi tenant missing")
)

// MaxROIWindowDays is the upper bound on the date window. Bounded so
// an unbounded scan cannot blow the p95 budget.
const MaxROIWindowDays = 366

// DefaultDeadStockMinAgeDays is the default floor for the
// /dead-stock filter. Items with no orders in the last N days are
// flagged as slow-moving.
const DefaultDeadStockMinAgeDays = 60

// ROIPeriodAllowed enumerates accepted period strings.
var ROIPeriodAllowed = map[string]int{
	"7d":  7,
	"30d": 30,
	"90d": 90,
}

// ROIFilter is the parsed query envelope.
type ROIFilter struct {
	TenantID   string
	From       time.Time
	To         time.Time
	Channel    string
	Product    string
	MinAgeDays int
	Dimensions []string
}

// ROIPoint is one tenant+channel+day rollup row.
type ROIPoint struct {
	Day                  time.Time `json:"day"`
	Channel              string    `json:"channel"`
	ProductID            string    `json:"product_id"`
	TotalRevenueAUDCents int64     `json:"total_revenue_aud_cents"`
	GrossProfitAUDCents  int64     `json:"gross_profit_aud_cents"`
	OrderCount           int64     `json:"order_count"`
	TotalCostAUDCents    int64     `json:"total_cost_aud_cents"`
	LastOrderAt          time.Time `json:"last_order_at,omitempty"`
}

// ROIHeatmapCell is one (channel, product) cell of the heatmap.
type ROIHeatmapCell struct {
	Channel    string  `json:"channel"`
	ProductID  string  `json:"product_id"`
	ROIPct     float64 `json:"roi_pct"`
	OrderCount int64   `json:"order_count"`
}

// ROIChannelBreakdown is one channel-level breakdown row.
type ROIChannelBreakdown struct {
	Channel    string  `json:"channel"`
	ROIPct     float64 `json:"roi_pct"`
	OrderCount int64   `json:"order_count"`
	NetRevenue int64   `json:"net_revenue_aud_cents"`
}

// ROIRepository is the small port the ROI handler reads through.
// Production wires a Postgres-backed implementation; tests use an
// in-memory one.
type ROIRepository interface {
	Heatmap(ctx context.Context, filter ROIFilter) ([]ROIPoint, error)
	DeadStock(ctx context.Context, filter ROIFilter) ([]ROIPoint, error)
	ByChannel(ctx context.Context, filter ROIFilter) ([]ROIPoint, error)
}

// ROIHandlerMetrics is the small port the handler emits a request
// duration histogram through.
type ROIHandlerMetrics interface {
	ObserveROIQueryDurationSeconds(durationSec float64)
}

// ROIHandlerConfig wires the handler.
type ROIHandlerConfig struct {
	Repository   ROIRepository
	TenantHeader string
	Now          func() time.Time
	Metrics      ROIHandlerMetrics
}

// ROIHandler is the EC-9-3 HTTP handler.
type ROIHandler struct {
	repo         ROIRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      ROIHandlerMetrics

	mu     sync.Mutex
	closed bool
}

// NewROIHandler constructs the handler.
func NewROIHandler(logger *slog.Logger, cfg ROIHandlerConfig) (*ROIHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: ROIRepository required", ErrROIHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ROIHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
	}, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *ROIHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes by URL suffix. Decomposed so the per-route
// helpers stay flat.
func (h *ROIHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/analytics/roi")
	switch strings.TrimSuffix(suffix, "/") {
	case "/heatmap":
		h.handleHeatmap(w, r)
	case "/dead-stock":
		h.handleDeadStock(w, r)
	case "/by-channel":
		h.handleByChannel(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown roi route: %s", r.URL.Path))
	}
}

func (h *ROIHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrROIHandlerClosed
	}
	return nil
}

func (h *ROIHandler) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseROIFilter(r, false)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.Heatmap(r.Context(), filter)
	if err != nil {
		h.logger.Error("roi.heatmap_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	cells := buildHeatmapCells(rows)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"cells":     cells,
	})
}

func (h *ROIHandler) handleDeadStock(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseROIFilter(r, true)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.DeadStock(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":       filter.TenantID,
		"min_age_days":    filter.MinAgeDays,
		"dead_stock_rows": rows,
		"count":           len(rows),
	})
}

func (h *ROIHandler) handleByChannel(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseROIFilter(r, false)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.ByChannel(r.Context(), filter)
	if err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"channels":  buildChannelBreakdown(rows),
	})
}

func (h *ROIHandler) parseROIFilter(r *http.Request, withMinAge bool) (ROIFilter, error) {
	tenantID, err := h.resolveROITenantID(r)
	if err != nil {
		return ROIFilter{}, err
	}
	q := r.URL.Query()
	from, to, err := parseROIWindow(q, h.now())
	if err != nil {
		return ROIFilter{}, err
	}
	minAge := DefaultDeadStockMinAgeDays
	if withMinAge {
		if raw := q.Get("min_age_days"); raw != "" {
			parsed, perr := strconv.Atoi(raw)
			if perr != nil || parsed <= 0 {
				return ROIFilter{}, fmt.Errorf("%w: invalid min_age_days", ErrInvalidPeriod)
			}
			minAge = parsed
		}
	}
	return ROIFilter{
		TenantID:   tenantID,
		From:       from,
		To:         to,
		Channel:    strings.TrimSpace(q.Get("channel")),
		Product:    strings.TrimSpace(q.Get("product")),
		MinAgeDays: minAge,
		Dimensions: parseDimensions(q.Get("dimensions")),
	}, nil
}

func (h *ROIHandler) resolveROITenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrROITenantMissing
}

func (h *ROIHandler) recordDuration(start time.Time) {
	if h.metrics == nil {
		return
	}
	h.metrics.ObserveROIQueryDurationSeconds(h.now().Sub(start).Seconds())
}

// parseROIWindow accepts either period=30d|90d|7d OR explicit
// from/to RFC3339 timestamps. Cyclomatic 5.
func parseROIWindow(q map[string][]string, now time.Time) (time.Time, time.Time, error) {
	if period := getQueryValue(q, "period"); period != "" {
		days, ok := ROIPeriodAllowed[period]
		if !ok {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: period %q not in {7d,30d,90d}", ErrInvalidPeriod, period)
		}
		to := now.UTC()
		from := to.Add(-time.Duration(days) * 24 * time.Hour)
		return from, to, nil
	}
	fromRaw := getQueryValue(q, "from")
	toRaw := getQueryValue(q, "to")
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from + to or period required", ErrInvalidPeriod)
	}
	from, err := parseRFC3339OrDate(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from invalid", ErrInvalidPeriod)
	}
	to, err := parseRFC3339OrDate(toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to invalid", ErrInvalidPeriod)
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to must be after from", ErrInvalidPeriod)
	}
	if to.Sub(from) > time.Duration(MaxROIWindowDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: window > %d days", ErrInvalidPeriod, MaxROIWindowDays)
	}
	return from, to, nil
}

func getQueryValue(q map[string][]string, key string) string {
	if v, ok := q[key]; ok && len(v) > 0 {
		return strings.TrimSpace(v[0])
	}
	return ""
}

func parseDimensions(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return []string{"channel", "product"}
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// buildHeatmapCells transforms repository rows into heatmap cells.
func buildHeatmapCells(rows []ROIPoint) []ROIHeatmapCell {
	cells := make([]ROIHeatmapCell, 0, len(rows))
	for _, r := range rows {
		cells = append(cells, ROIHeatmapCell{
			Channel:    r.Channel,
			ProductID:  r.ProductID,
			ROIPct:     computeROIPct(r),
			OrderCount: r.OrderCount,
		})
	}
	return cells
}

// buildChannelBreakdown aggregates the rollup rows by channel.
func buildChannelBreakdown(rows []ROIPoint) []ROIChannelBreakdown {
	type acc struct {
		revenue int64
		cost    int64
		orders  int64
	}
	bucket := map[string]*acc{}
	for _, r := range rows {
		a, ok := bucket[r.Channel]
		if !ok {
			a = &acc{}
			bucket[r.Channel] = a
		}
		a.revenue += r.TotalRevenueAUDCents
		a.cost += r.TotalCostAUDCents
		a.orders += r.OrderCount
	}
	out := make([]ROIChannelBreakdown, 0, len(bucket))
	for ch, a := range bucket {
		var roi float64
		if a.cost > 0 {
			roi = float64(a.revenue-a.cost) / float64(a.cost) * 100
		}
		out = append(out, ROIChannelBreakdown{
			Channel:    ch,
			ROIPct:     roi,
			OrderCount: a.orders,
			NetRevenue: a.revenue - a.cost,
		})
	}
	return out
}

// computeROIPct computes the ROI percentage for one row.
func computeROIPct(r ROIPoint) float64 {
	if r.TotalCostAUDCents <= 0 {
		return 0
	}
	return float64(r.TotalRevenueAUDCents-r.TotalCostAUDCents) / float64(r.TotalCostAUDCents) * 100
}
