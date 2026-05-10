// File scope: v3.9.1 EC-9-4 channel content performance analytics
// HTTP handler.
//
// Endpoints (REST + JSON, all tenant-scoped):
//
//   - GET /api/v1/analytics/channel-content?tenant_id=...&channel=tiktok&period=30d
//     -> per-channel content KPIs (post count, total engagement,
//     avg engagement, conversion rate, GMV attribution).
//   - GET /api/v1/analytics/channel-content/top?tenant_id=...&period=30d&limit=10
//     -> top-N performers across the period.
//
// Reads through the small ChannelContentRepository port. Production
// wires a Postgres-backed implementation that hits the
// channel_content_daily_rollup materialized view (migration 0024)
// joined with content_performance_history (migration 0022) + orders
// (v3.5.0 EC-7-1). Tests use an in-memory implementation.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 17-sprint streak target):
//
//   - ServeHTTP                -> route by suffix (cyclomatic 4)
//   - parseChannelFilter       -> validate query (cyclomatic 5)
//   - handleByChannel          -> repo + write (cyclomatic 3)
//   - handleTopPerformers      -> repo + sort + limit + write (cyclomatic 4)
//
// Each helper stays under cyclomatic 6.
package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// EC-9-4 typed sentinels.
var (
	// ErrChannelContentHandlerUnconfigured is returned by
	// NewChannelContentHandler when a required dependency is missing.
	ErrChannelContentHandlerUnconfigured = errors.New("handler: channel content handler unconfigured")

	// ErrChannelContentHandlerClosed is returned after Close.
	ErrChannelContentHandlerClosed = errors.New("handler: channel content handler closed")

	// ErrChannelContentInvalidPeriod is returned when the period
	// query parameter is missing, malformed, or out of bounds.
	ErrChannelContentInvalidPeriod = errors.New("handler: channel content invalid period")

	// ErrInvalidChannelFilter is returned when the channel filter
	// names a value outside the accepted set
	// (tiktok|rednote|facebook|instagram|pinterest).
	ErrInvalidChannelFilter = errors.New("handler: invalid channel filter")

	// ErrInsufficientContentData is returned when the rollup
	// contains too few rows to produce a meaningful response.
	ErrInsufficientContentData = errors.New("handler: insufficient content data")

	// ErrChannelContentTenantMissing is returned when the tenant_id
	// cannot be derived from the request.
	ErrChannelContentTenantMissing = errors.New("handler: channel content tenant missing")
)

// ChannelContentPeriodAllowed enumerates accepted period strings.
// Mirrors ROIPeriodAllowed + MarginPeriodAllowed so operators pivot
// one period across all three dashboards.
var ChannelContentPeriodAllowed = map[string]int{
	"7d":  7,
	"30d": 30,
	"90d": 90,
}

// MaxChannelContentWindowDays caps the window so an unbounded scan
// cannot blow the p95 budget.
const MaxChannelContentWindowDays = 366

// DefaultTopPerformersLimit is the default for /top when the limit
// query parameter is absent.
const DefaultTopPerformersLimit = 10

// MaxTopPerformersLimit caps /top so a malicious caller cannot ask
// for unbounded rows.
const MaxTopPerformersLimit = 100

// ChannelContentAllowedChannels enumerates the channel names the
// handler accepts. Stub channels (instagram + pinterest) are
// included for forward compatibility; the rollup may have zero
// rows for them today.
var ChannelContentAllowedChannels = map[string]struct{}{
	"tiktok":    {},
	"rednote":   {},
	"facebook":  {},
	"instagram": {},
	"pinterest": {},
}

// ChannelContentFilter is the parsed query envelope.
type ChannelContentFilter struct {
	TenantID string
	From     time.Time
	To       time.Time
	Channel  string
	Limit    int
}

// ChannelContentRow is one (tenant, day, channel, content_type) KPI
// row. The KPI shape mirrors what the EC-9-4 dashboard renders.
type ChannelContentRow struct {
	Day                 time.Time `json:"day"`
	Channel             string    `json:"channel"`
	ContentType         string    `json:"content_type"`
	PostCount           int64     `json:"post_count"`
	TotalEngagement     float64   `json:"total_engagement"`
	AvgEngagement       float64   `json:"avg_engagement_per_post"`
	ConversionCount     int64     `json:"conversion_count"`
	ConversionRate      float64   `json:"conversion_rate"`
	GMVAttributionCents int64     `json:"gmv_attribution_aud_cents"`
}

// ChannelContentTopPerformer is one top-N row produced by the
// /top endpoint. Sorted by total engagement (descending) with a
// secondary sort key on post_count to break ties deterministically.
type ChannelContentTopPerformer struct {
	Channel         string  `json:"channel"`
	ContentType     string  `json:"content_type"`
	PostCount       int64   `json:"post_count"`
	TotalEngagement float64 `json:"total_engagement"`
	AvgEngagement   float64 `json:"avg_engagement_per_post"`
	ConversionRate  float64 `json:"conversion_rate"`
	GMVAttribution  int64   `json:"gmv_attribution_aud_cents"`
}

// ChannelContentRepository is the small port the handler reads
// through. Production wires a Postgres-backed implementation; tests
// pass an in-memory one.
type ChannelContentRepository interface {
	ByChannel(ctx context.Context, filter ChannelContentFilter) ([]ChannelContentRow, error)
	TopPerformers(ctx context.Context, filter ChannelContentFilter) ([]ChannelContentTopPerformer, error)
}

// ChannelContentHandlerMetrics is the small port the handler emits
// a request duration histogram through.
type ChannelContentHandlerMetrics interface {
	ObserveChannelContentQueryDuration(durationSec float64)
}

// ChannelContentHandlerConfig wires the handler.
type ChannelContentHandlerConfig struct {
	Repository   ChannelContentRepository
	TenantHeader string
	Now          func() time.Time
	Metrics      ChannelContentHandlerMetrics
}

// ChannelContentHandler is the EC-9-4 HTTP handler.
type ChannelContentHandler struct {
	repo         ChannelContentRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      ChannelContentHandlerMetrics

	mu     sync.Mutex
	closed bool
}

// NewChannelContentHandler constructs the handler.
func NewChannelContentHandler(logger *slog.Logger, cfg ChannelContentHandlerConfig) (*ChannelContentHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: ChannelContentRepository required", ErrChannelContentHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &ChannelContentHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
	}, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *ChannelContentHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes by URL suffix. Cyclomatic 4 (matches the GMV /
// ROI / Margin pattern).
func (h *ChannelContentHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
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
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/analytics/channel-content")
	switch strings.TrimSuffix(suffix, "/") {
	case "":
		h.handleByChannel(w, r)
	case "/top":
		h.handleTopPerformers(w, r)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown channel-content route: %s", r.URL.Path))
	}
}

func (h *ChannelContentHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrChannelContentHandlerClosed
	}
	return nil
}

func (h *ChannelContentHandler) handleByChannel(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseChannelFilter(r, false)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.ByChannel(r.Context(), filter)
	if err != nil {
		h.logger.Error("channel_content.by_channel_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": filter.TenantID,
		"from":      filter.From,
		"to":        filter.To,
		"channel":   filter.Channel,
		"rows":      rows,
		"summary":   summariseChannelContent(rows),
	})
}

// handleTopPerformers serves the /top endpoint. Cyclomatic 4: filter
// + repo + sort + write. Sort logic stays in helper to keep this
// shallow.
func (h *ChannelContentHandler) handleTopPerformers(w http.ResponseWriter, r *http.Request) {
	filter, err := h.parseChannelFilter(r, true)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	rows, err := h.repo.TopPerformers(r.Context(), filter)
	if err != nil {
		h.logger.Error("channel_content.top_performers_failed", "tenant_id", filter.TenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	rows = sortTopPerformers(rows, filter.Limit)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":      filter.TenantID,
		"from":           filter.From,
		"to":             filter.To,
		"limit":          filter.Limit,
		"top_performers": rows,
	})
}

// parseChannelFilter validates query arguments. Cyclomatic 5.
func (h *ChannelContentHandler) parseChannelFilter(r *http.Request, withLimit bool) (ChannelContentFilter, error) {
	tenantID, err := h.resolveChannelContentTenantID(r)
	if err != nil {
		return ChannelContentFilter{}, err
	}
	q := r.URL.Query()
	from, to, err := parseChannelContentWindow(q, h.now())
	if err != nil {
		return ChannelContentFilter{}, err
	}
	channel := strings.TrimSpace(q.Get("channel"))
	if channel != "" {
		if _, ok := ChannelContentAllowedChannels[strings.ToLower(channel)]; !ok {
			return ChannelContentFilter{}, fmt.Errorf("%w: %q", ErrInvalidChannelFilter, channel)
		}
	}
	limit := DefaultTopPerformersLimit
	if withLimit {
		if raw := q.Get("limit"); raw != "" {
			parsed, perr := strconv.Atoi(raw)
			if perr != nil || parsed <= 0 {
				return ChannelContentFilter{}, fmt.Errorf("%w: invalid limit", ErrChannelContentInvalidPeriod)
			}
			if parsed > MaxTopPerformersLimit {
				parsed = MaxTopPerformersLimit
			}
			limit = parsed
		}
	}
	return ChannelContentFilter{
		TenantID: tenantID,
		From:     from,
		To:       to,
		Channel:  strings.ToLower(channel),
		Limit:    limit,
	}, nil
}

func (h *ChannelContentHandler) resolveChannelContentTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrChannelContentTenantMissing
}

func (h *ChannelContentHandler) recordDuration(start time.Time) {
	if h.metrics == nil {
		return
	}
	h.metrics.ObserveChannelContentQueryDuration(h.now().Sub(start).Seconds())
}

// parseChannelContentWindow accepts either period=7d|30d|90d OR
// explicit from/to RFC3339 timestamps. Cyclomatic 5.
func parseChannelContentWindow(q map[string][]string, now time.Time) (time.Time, time.Time, error) {
	if period := getQueryValue(q, "period"); period != "" {
		days, ok := ChannelContentPeriodAllowed[period]
		if !ok {
			return time.Time{}, time.Time{}, fmt.Errorf("%w: period %q not in {7d,30d,90d}", ErrChannelContentInvalidPeriod, period)
		}
		to := now.UTC()
		from := to.Add(-time.Duration(days) * 24 * time.Hour)
		return from, to, nil
	}
	fromRaw := getQueryValue(q, "from")
	toRaw := getQueryValue(q, "to")
	if fromRaw == "" || toRaw == "" {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from + to or period required", ErrChannelContentInvalidPeriod)
	}
	from, err := parseRFC3339OrDate(fromRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: from invalid", ErrChannelContentInvalidPeriod)
	}
	to, err := parseRFC3339OrDate(toRaw)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to invalid", ErrChannelContentInvalidPeriod)
	}
	if !to.After(from) {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: to must be after from", ErrChannelContentInvalidPeriod)
	}
	if to.Sub(from) > time.Duration(MaxChannelContentWindowDays)*24*time.Hour {
		return time.Time{}, time.Time{}, fmt.Errorf("%w: window > %d days", ErrChannelContentInvalidPeriod, MaxChannelContentWindowDays)
	}
	return from, to, nil
}

// summariseChannelContent rolls the per-channel slice into a
// totals envelope. Pure function so it stays under cyclomatic 3.
func summariseChannelContent(rows []ChannelContentRow) map[string]any {
	var (
		posts          int64
		engagement     float64
		conversions    int64
		gmvAttribution int64
	)
	for _, r := range rows {
		posts += r.PostCount
		engagement += r.TotalEngagement
		conversions += r.ConversionCount
		gmvAttribution += r.GMVAttributionCents
	}
	avg := 0.0
	if posts > 0 {
		avg = engagement / float64(posts)
	}
	return map[string]any{
		"post_count":                posts,
		"total_engagement":          engagement,
		"avg_engagement_per_post":   avg,
		"conversion_count":          conversions,
		"gmv_attribution_aud_cents": gmvAttribution,
	}
}

// sortTopPerformers sorts the top-performer slice in place by
// (TotalEngagement desc, PostCount desc, Channel asc) and trims it
// to the limit. Pure function so it stays cyclomatic 3.
func sortTopPerformers(rows []ChannelContentTopPerformer, limit int) []ChannelContentTopPerformer {
	sort.SliceStable(rows, func(i, j int) bool {
		if rows[i].TotalEngagement != rows[j].TotalEngagement {
			return rows[i].TotalEngagement > rows[j].TotalEngagement
		}
		if rows[i].PostCount != rows[j].PostCount {
			return rows[i].PostCount > rows[j].PostCount
		}
		return rows[i].Channel < rows[j].Channel
	})
	if limit > 0 && len(rows) > limit {
		rows = rows[:limit]
	}
	return rows
}
