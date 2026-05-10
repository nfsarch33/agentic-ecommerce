// File scope: v3.9.0 EC-6-5 margin dashboard analytics HTTP handler.
//
// Endpoints (REST + JSON, all tenant-scoped):
//
//   - GET /api/v1/analytics/margin/dashboard?tenant_id=...&period=30d
//     -> unified margin view (revenue, costs, ROI, competitor positioning)
//   - GET /api/v1/analytics/margin/alerts?tenant_id=...
//     -> pending margin alerts (sub-floor, competitor undercut, dead-stock)
//   - GET /api/v1/analytics/margin/forecast?tenant_id=...&period=30d
//     -> forward 30-day forecast based on EMA of recent gross margin
//
// Reads through the small MarginRepository port. Production wires
// a Postgres-backed implementation that joins:
//   - orders + supplier_cost_baselines (v3.5.0 EC-6-1)
//   - shipping_labels (v3.8.0 EC-7-3)
//   - roi_daily_rollup (v3.8.0 EC-9-3)
//   - competitor_prices (v3.9.0 EC-6-4)
//
// Tests use an in-memory implementation.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 16-sprint streak; v3.9.0 sprint 16 target):
//
//   - ServeHTTP            -> route by suffix (cyclomatic 4)
//   - parseMarginFilter    -> validate query (cyclomatic 5)
//   - handleDashboard      -> repo + write (cyclomatic 3)
//   - handleAlerts         -> repo + write (cyclomatic 3)
//   - handleForecast       -> repo + write (cyclomatic 3)
//
// Each helper stays under cyclomatic 6.
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

// EC-6-5 typed sentinels.
var (
	// ErrMarginHandlerUnconfigured is returned by NewMarginHandler
	// when a required dependency is missing.
	ErrMarginHandlerUnconfigured = errors.New("handler: margin handler unconfigured")

	// ErrMarginHandlerClosed is returned after Close.
	ErrMarginHandlerClosed = errors.New("handler: margin handler closed")

	// ErrMarginInvalidPeriod is returned when the period query
	// parameter is missing, malformed, or out of bounds.
	ErrMarginInvalidPeriod = errors.New("handler: margin invalid period")

	// ErrMarginTenantMissing is returned when the tenant_id cannot
	// be derived from the request.
	ErrMarginTenantMissing = errors.New("handler: margin tenant missing")
)

// MarginPeriodAllowed enumerates accepted period strings. Mirrors
// ROIPeriodAllowed so operators can pivot one period across both
// dashboards.
var MarginPeriodAllowed = map[string]int{
	"7d":  7,
	"30d": 30,
	"90d": 90,
}

// MaxMarginWindowDays caps the window so an unbounded scan cannot
// blow the p95 budget.
const MaxMarginWindowDays = 366

// MarginFilter is the parsed query envelope.
type MarginFilter struct {
	TenantID string
	From     time.Time
	To       time.Time
	Channel  string
}

// MarginDashboardSnapshot is the unified margin view returned by
// the /dashboard endpoint.
type MarginDashboardSnapshot struct {
	RevenueAUDCents       int64   `json:"revenue_aud_cents"`
	SupplierCostAUDCents  int64   `json:"supplier_cost_aud_cents"`
	ShippingCostAUDCents  int64   `json:"shipping_cost_aud_cents"`
	PlatformFeesAUDCents  int64   `json:"platform_fees_aud_cents"`
	NetMarginAUDCents     int64   `json:"net_margin_aud_cents"`
	NetMarginPct          float64 `json:"net_margin_pct"`
	ROIPct                float64 `json:"roi_pct"`
	OrderCount            int64   `json:"order_count"`
	CompetitorAvgAUDCents int64   `json:"competitor_avg_aud_cents"`
	CompetitorPositioning string  `json:"competitor_positioning"` // above|below|parity
}

// MarginAlert is a single alert surfaced by the /alerts endpoint.
type MarginAlert struct {
	ProductID string    `json:"product_id"`
	Channel   string    `json:"channel,omitempty"`
	Severity  string    `json:"severity"` // info|warning|critical
	Reason    string    `json:"reason"`
	DeltaPct  float64   `json:"delta_pct,omitempty"`
	CreatedAt time.Time `json:"created_at,omitempty"`
}

// MarginForecast is the forward-looking margin projection.
type MarginForecast struct {
	ForecastAUDCents   int64   `json:"forecast_aud_cents"`
	LowerBoundAUDCents int64   `json:"lower_bound_aud_cents"`
	UpperBoundAUDCents int64   `json:"upper_bound_aud_cents"`
	ConfidencePct      float64 `json:"confidence_pct"`
	BasedOnDays        int     `json:"based_on_days"`
}

// MarginRepository is the small port the margin handler reads
// through. Production wires a Postgres-backed implementation; tests
// pass an in-memory one.
type MarginRepository interface {
	Dashboard(ctx context.Context, filter MarginFilter) (MarginDashboardSnapshot, error)
	Alerts(ctx context.Context, filter MarginFilter) ([]MarginAlert, error)
	Forecast(ctx context.Context, filter MarginFilter) (MarginForecast, error)
}

// MarginHandlerMetrics is the small port the handler emits a
// request duration histogram through.
type MarginHandlerMetrics interface {
	ObserveMarginDashboardDuration(durationSec float64)
}

// MarginHandlerConfig wires the handler.
type MarginHandlerConfig struct {
	Repository   MarginRepository
	TenantHeader string
	Now          func() time.Time
	Metrics      MarginHandlerMetrics
}

// MarginHandler is the EC-6-5 HTTP handler.
type MarginHandler struct {
	repo         MarginRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      MarginHandlerMetrics

	mu     sync.Mutex
	closed bool
}

// NewMarginHandler constructs the handler.
func NewMarginHandler(logger *slog.Logger, cfg MarginHandlerConfig) (*MarginHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: MarginRepository required", ErrMarginHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &MarginHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
	}, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *MarginHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes by URL suffix. Cyclomatic 4 (matches the GMV/ROI
// pattern).
func (h *MarginHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/analytics/margin")
	switch strings.TrimSuffix(suffix, "/") {
	case "/dashboard":
		h.handleDashboard(w, r)
	case "/alerts":
		h.handleAlerts(w, r)
	case "/forecast":
		h.handleForecast(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown margin route: %s", r.URL.Path))
	}
}

func (h *MarginHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrMarginHandlerClosed
	}
	return nil
}

func (h *MarginHandler) handleDashboard(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseMarginFilter(r, true)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	snap, err := h.repo.Dashboard(r.Context(), filter)
	if err != nil {
		h.logger.Error("margin.dashboard_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"channel":   filter.Channel,
		"dashboard": snap,
	})
}

func (h *MarginHandler) handleAlerts(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseMarginFilter(r, false)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	alerts, err := h.repo.Alerts(r.Context(), filter)
	if err != nil {
		h.logger.Error("margin.alerts_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"alerts":    alerts,
		"count":     len(alerts),
	})
}

func (h *MarginHandler) handleForecast(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseMarginFilter(r, true)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	forecast, err := h.repo.Forecast(r.Context(), filter)
	if err != nil {
		h.logger.Error("margin.forecast_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":             filter.TenantID,
		"from":                  filter.From,
		"to":                    filter.To,
		"forecast_aud_cents":    forecast.ForecastAUDCents,
		"lower_bound_aud_cents": forecast.LowerBoundAUDCents,
		"upper_bound_aud_cents": forecast.UpperBoundAUDCents,
		"confidence_pct":        forecast.ConfidencePct,
		"based_on_days":         forecast.BasedOnDays,
	})
}

// parseMarginFilter validates query arguments. Cyclomatic 5.
// When requirePeriod is true, the request MUST supply period or
// from+to; otherwise an empty window is acceptable (used by
// /alerts which returns currently pending alerts).
func (h *MarginHandler) parseMarginFilter(r *http.Request, requirePeriod bool) (MarginFilter, error) {
	tenantID, err := h.resolveMarginTenantID(r)
	if err != nil {
		return MarginFilter{}, err
	}
	q := r.URL.Query()
	var from, to time.Time
	if requirePeriod {
		var werr error
		from, to, werr = parseMarginWindow(q, h.now())
		if werr != nil {
			return MarginFilter{}, werr
		}
	}
	channel := strings.TrimSpace(q.Get("channel"))
	return MarginFilter{
		TenantID: tenantID,
		From:     from,
		To:       to,
		Channel:  channel,
	}, nil
}

func (h *MarginHandler) resolveMarginTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrMarginTenantMissing
}

func (h *MarginHandler) recordDuration(start time.Time) {
	if h.metrics == nil {
		return
	}
	h.metrics.ObserveMarginDashboardDuration(h.now().Sub(start).Seconds())
}

// parseMarginWindow accepts either period=30d|90d|7d OR explicit
// from/to RFC3339 timestamps. Cyclomatic 5.
func parseMarginWindow(q map[string][]string, now time.Time) (time.Time, time.Time, error) {
	if period := getQueryValue(q, "period"); period != "" {
		days, ok := MarginPeriodAllowed[period]
		if !ok {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: period %q not in {7d,30d,90d}", ErrMarginInvalidPeriod, period)
		}
		to := now.UTC()
		from := to.Add(-time.Duration(days) * 24 * time.Hour)
		return from, to, nil
	}
	fromRaw := getQueryValue(q, "from")
	toRaw := getQueryValue(q, "to")
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from + to or period required", ErrMarginInvalidPeriod)
	}
	from, err := parseRFC3339OrDate(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from invalid", ErrMarginInvalidPeriod)
	}
	to, err := parseRFC3339OrDate(toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to invalid", ErrMarginInvalidPeriod)
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to must be after from", ErrMarginInvalidPeriod)
	}
	if to.Sub(from) > time.Duration(MaxMarginWindowDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: window > %d days", ErrMarginInvalidPeriod, MaxMarginWindowDays)
	}
	return from, to, nil
}
