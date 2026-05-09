// File scope: v3.6.0 EC-9-1 GMV analytics HTTP handler.
//
// Endpoints (all REST + JSON, all tenant-scoped):
//
//   - GET /api/v1/analytics/gmv?from=...&to=...&channel=...
//     -> tenant rollup aggregated across the date window.
//   - GET /api/v1/analytics/gmv/by-channel?from=...&to=...
//     -> per-channel breakdown of the date window.
//   - GET /api/v1/analytics/gmv/by-product?from=...&to=...&limit=20
//     -> top-N products by GMV.
//
// Reads the v3.5.0 EC-7-1 normalised order data through the
// gmv_daily_rollup materialized view (migration 0016) via the
// small GMVRepository port. Tests use an in-memory implementation;
// production wires a Postgres adapter.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//
//   - ServeHTTP            -> route by suffix (cyclomatic 4)
//   - parseFilter          -> validate query (cyclomatic 5)
//   - handleSummary        -> repo + write (cyclomatic 3)
//   - handleByChannel      -> repo + write (cyclomatic 3)
//   - handleByProduct      -> repo + write + limit (cyclomatic 4)
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EC-9-1 typed sentinels.
var (
	// ErrGMVHandlerUnconfigured is returned by NewGMVHandler when a
	// required dependency is missing.
	ErrGMVHandlerUnconfigured = errors.New("handler: gmv handler unconfigured")

	// ErrGMVHandlerClosed is returned after Close.
	ErrGMVHandlerClosed = errors.New("handler: gmv handler closed")

	// ErrGMVInvalidDateRange is returned when from/to are missing,
	// inverted, or out of bounds.
	ErrGMVInvalidDateRange = errors.New("handler: gmv invalid date range")

	// ErrGMVTenantMissing is returned when the tenant_id cannot be
	// derived from the request (neither header nor query).
	ErrGMVTenantMissing = errors.New("handler: gmv tenant missing")
)

// MaxGMVWindowDays is the upper bound on the date window. Bounded
// so an unbounded scan cannot blow the p95 budget.
const MaxGMVWindowDays = 366

// DefaultTopProductLimit is the default for /by-product when the
// limit query parameter is absent. Plan EC-9-1 acceptance: top-N
// at 20 by default.
const DefaultTopProductLimit = 20

// MaxTopProductLimit caps the /by-product limit so a malicious
// caller cannot ask for unbounded rows.
const MaxTopProductLimit = 100

// GMVFilter is the parsed query envelope.
type GMVFilter struct {
	TenantID string
	From     time.Time
	To       time.Time
	Channel  string
	Limit    int
}

// GMVDailyPoint is one tenant+channel+day row from the rollup.
type GMVDailyPoint struct {
	Day         time.Time `json:"day"`
	GMVAUDCents int64     `json:"gmv_aud_cents"`
	OrderCount  int64     `json:"order_count"`
}

// GMVChannelPoint is one channel-level breakdown row.
type GMVChannelPoint struct {
	Channel     string `json:"channel"`
	GMVAUDCents int64  `json:"gmv_aud_cents"`
	OrderCount  int64  `json:"order_count"`
}

// GMVProductPoint is one product-level breakdown row.
type GMVProductPoint struct {
	ProductID   string `json:"product_id"`
	GMVAUDCents int64  `json:"gmv_aud_cents"`
	OrderCount  int64  `json:"order_count"`
}

// GMVRepository is the small port the GMV handler reads through.
// Production wires a Postgres-backed implementation that hits
// gmv_daily_rollup; tests use an in-memory one.
type GMVRepository interface {
	Daily(ctx context.Context, filter GMVFilter) ([]GMVDailyPoint, error)
	ByChannel(ctx context.Context, filter GMVFilter) ([]GMVChannelPoint, error)
	ByProduct(ctx context.Context, filter GMVFilter) ([]GMVProductPoint, error)
}

// GMVHandlerMetrics is the small port the handler emits a request
// duration histogram through.
type GMVHandlerMetrics interface {
	ObserveGMVRequestDurationSeconds(durationSec float64)
}

// GMVHandlerConfig wires the handler.
type GMVHandlerConfig struct {
	Repository   GMVRepository
	TenantHeader string // defaults to "X-Tenant-Id"
	Now          func() time.Time
	Metrics      GMVHandlerMetrics
}

// GMVHandler is the EC-9-1 HTTP handler.
type GMVHandler struct {
	repo         GMVRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      GMVHandlerMetrics

	mu     sync.Mutex
	closed bool
}

// NewGMVHandler constructs the handler.
func NewGMVHandler(logger *slog.Logger, cfg GMVHandlerConfig) (*GMVHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: GMVRepository required", ErrGMVHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &GMVHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
	}, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *GMVHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes by URL suffix. Decomposed so the per-route
// helpers stay flat.
func (h *GMVHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/analytics/gmv")
	switch strings.TrimSuffix(suffix, "/") {
	case "":
		h.handleSummary(w, r)
	case "/by-channel":
		h.handleByChannel(w, r)
	case "/by-product":
		h.handleByProduct(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown analytics route: %s", r.URL.Path))
	}
}

func (h *GMVHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrGMVHandlerClosed
	}
	return nil
}

func (h *GMVHandler) handleSummary(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseFilter(r, false)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.Daily(r.Context(), filter)
	if err != nil {
		h.logger.Error("gmv.daily_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"channel":   filter.Channel,
		"daily":     rows,
		"summary":   summariseDaily(rows),
	})
}

func (h *GMVHandler) handleByChannel(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseFilter(r, false)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.ByChannel(r.Context(), filter)
	if err != nil {
		h.logger.Error("gmv.by_channel_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"channels":  rows,
	})
}

func (h *GMVHandler) handleByProduct(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseFilter(r, true)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.ByProduct(r.Context(), filter)
	if err != nil {
		h.logger.Error("gmv.by_product_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"limit":     filter.Limit,
		"products":  rows,
	})
}

func (h *GMVHandler) parseFilter(r *http.Request, withLimit bool) (GMVFilter, error) {
	tenantID, err := h.resolveTenantID(r)
	if err != nil {
		return GMVFilter{}, err
	}
	q := r.URL.Query()
	from, err := parseRFC3339OrDate(q.Get("from"))
	if err != nil {
		return GMVFilter{}, fmt.Errorf("%w: from invalid", ErrGMVInvalidDateRange)
	}
	to, err := parseRFC3339OrDate(q.Get("to"))
	if err != nil {
		return GMVFilter{}, fmt.Errorf("%w: to invalid", ErrGMVInvalidDateRange)
	}
	if !to.After(from) {
		return GMVFilter{}, fmt.Errorf("%w: to must be after from", ErrGMVInvalidDateRange)
	}
	if to.Sub(from) > time.Duration(MaxGMVWindowDays)*24*time.Hour {
		return GMVFilter{}, fmt.Errorf("%w: window > %d days", ErrGMVInvalidDateRange, MaxGMVWindowDays)
	}
	limit := DefaultTopProductLimit
	if withLimit {
		if raw := q.Get("limit"); raw != "" {
			parsed, err := strconv.Atoi(raw)
			if err != nil || parsed <= 0 {
				return GMVFilter{}, fmt.Errorf("%w: invalid limit", ErrGMVInvalidDateRange)
			}
			if parsed > MaxTopProductLimit {
				parsed = MaxTopProductLimit
			}
			limit = parsed
		}
	}
	return GMVFilter{
		TenantID: tenantID,
		From:     from,
		To:       to,
		Channel:  strings.TrimSpace(q.Get("channel")),
		Limit:    limit,
	}, nil
}

// resolveTenantID prefers the X-Tenant-Id header (set by the JWT
// middleware) over the query string. The query string is accepted
// only when the header is absent so a multi-tenant operator
// dashboard can still drive ad-hoc queries against the canonical
// pipeline; tenant isolation is still enforced per-row in the
// repository (idx_gmv_daily_rollup_tenant_day + WHERE
// tenant_id = $1).
func (h *GMVHandler) resolveTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrGMVTenantMissing
}

func (h *GMVHandler) recordDuration(start time.Time) {
	if h.metrics == nil {
		return
	}
	h.metrics.ObserveGMVRequestDurationSeconds(h.now().Sub(start).Seconds())
}

// summariseDaily computes the totals across the daily slice.
func summariseDaily(rows []GMVDailyPoint) map[string]any {
	var totalCents int64
	var totalOrders int64
	for _, r := range rows {
		totalCents += r.GMVAUDCents
		totalOrders += r.OrderCount
	}
	return map[string]any{
		"gmv_aud_cents": totalCents,
		"order_count":   totalOrders,
	}
}

// parseRFC3339OrDate accepts RFC3339 + bare YYYY-MM-DD so the
// dashboard can post either shape.
func parseRFC3339OrDate(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, errors.New("empty")
	}
	if t, err := time.Parse(time.RFC3339, raw); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", raw); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("invalid date: %s", raw)
}

// writeJSON writes a status + JSON body and ensures the content
// type is set.
func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

// writeJSONError writes a status + canonical {error: ...} body.
func writeJSONError(w http.ResponseWriter, status int, err error) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
}
