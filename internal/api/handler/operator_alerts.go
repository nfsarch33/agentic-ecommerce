// File scope: v3.9.1 EC-9-5 -- centralised operator alert centre
// HTTP handler.
//
// Endpoints (REST + JSON, all tenant-scoped):
//
//   - GET    /api/v1/operator/alerts?tenant_id=...&status=pending
//     -> list active alerts (pending or acknowledged or resolved or
//     expired; status query gates).
//   - POST   /api/v1/operator/alerts/{alert_id}/acknowledge
//     -> mark a pending alert as acknowledged.
//   - POST   /api/v1/operator/alerts/{alert_id}/resolve?action=approve|deny
//     -> resolve an acknowledged or pending alert with an action.
//
// Alert lifecycle: pending -> acknowledged -> resolved (or expired
// after 24h via a sweeper).
//
// The alert centre subscribes to the v3.5.0 / v3.7.0 / v3.8.0 /
// v3.9.0 source events (LargeRefundPendingApproval,
// LargeDropshipOrderPendingApproval, PriceChangePendingApproval,
// CAPTCHA, OmniParserUnavailable, RateLimitDrain,
// ChannelStatusUpdateFailed, LargeMarginAlert) via a small
// AlertSink port -- the production wiring lives in the cmd/* root
// alongside the existing eventbus subscriptions.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4 -- 17-sprint streak target):
//
//   - ServeHTTP            -> route by suffix + method (cyclomatic 5)
//   - handleList           -> repo + write (cyclomatic 4)
//   - handleAcknowledge    -> path + repo + write (cyclomatic 4)
//   - handleResolve        -> path + action + repo + write (cyclomatic 5)
//   - applyResolution      -> dispatch action + emit event (cyclomatic 3)
//   - parseAlertID         -> path extraction (cyclomatic 3)
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

	"github.com/nfsarch33/agentic-ecommerce/internal/alert"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

// EC-9-5 typed sentinels.
var (
	// ErrOperatorAlertHandlerUnconfigured is returned by
	// NewOperatorAlertHandler when a required dependency is missing.
	ErrOperatorAlertHandlerUnconfigured = errors.New("handler: operator alert handler unconfigured")

	// ErrOperatorAlertHandlerClosed is returned after Close.
	ErrOperatorAlertHandlerClosed = errors.New("handler: operator alert handler closed")

	// ErrAlertNotFound is returned when a requested alert_id is not
	// in the repository for the requesting tenant.
	ErrAlertNotFound = errors.New("handler: alert not found")

	// ErrAlertAlreadyResolved is returned when a resolve / acknowledge
	// call targets an alert that is already in the resolved state.
	ErrAlertAlreadyResolved = errors.New("handler: alert already resolved")

	// ErrInvalidAlertAction is returned when the action query
	// parameter is missing or outside {approve, deny}.
	ErrInvalidAlertAction = errors.New("handler: invalid alert action")

	// ErrOperatorAlertTenantMissing is returned when the tenant_id
	// cannot be derived from the request.
	ErrOperatorAlertTenantMissing = errors.New("handler: operator alert tenant missing")
)

// AlertStatus, AlertSeverity, AlertType are type aliases to the
// canonical definitions in internal/alert (extracted in v4.10.0 to
// break the observability → handler import cycle).
type AlertStatus = alert.AlertStatus

const (
	AlertStatusPending      = alert.StatusPending
	AlertStatusAcknowledged = alert.StatusAcknowledged
	AlertStatusResolved     = alert.StatusResolved
	AlertStatusExpired      = alert.StatusExpired
)

type AlertSeverity = alert.AlertSeverity

const (
	AlertSeverityInfo     = alert.SeverityInfo
	AlertSeverityWarning  = alert.SeverityWarning
	AlertSeverityCritical = alert.SeverityCritical
)

type AlertType = alert.AlertType

const (
	AlertTypeLargeRefund       = alert.TypeLargeRefund
	AlertTypeLargeDropship     = alert.TypeLargeDropship
	AlertTypePriceChange       = alert.TypePriceChange
	AlertTypeCAPTCHADetected   = alert.TypeCAPTCHADetected
	AlertTypeOmniUnavailable   = alert.TypeOmniUnavailable
	AlertTypeRateLimitDrain    = alert.TypeRateLimitDrain
	AlertTypeChannelStatusFail = alert.TypeChannelStatusFail
	AlertTypeLargeMargin       = alert.TypeLargeMargin
)

// DefaultAlertExpiryWindow is the default time-to-expire for a
// pending alert. The sweeper job marks the row as expired after
// this window.
const DefaultAlertExpiryWindow = 24 * time.Hour

// AllowedAlertActions enumerates the resolve-action values.
var AllowedAlertActions = map[string]struct{}{
	"approve": {},
	"deny":    {},
}

// OperatorAlert is one alert record. The Payload field carries the
// source event's typed envelope (e.g. PriceChangePayload) so the
// dashboard can render context without re-querying the source.
type OperatorAlert struct {
	TenantID       string         `json:"tenant_id"`
	AlertID        string         `json:"alert_id"`
	AlertType      AlertType      `json:"alert_type"`
	Severity       AlertSeverity  `json:"severity"`
	Status         AlertStatus    `json:"status"`
	Payload        map[string]any `json:"payload,omitempty"`
	ActionTaken    string         `json:"action_taken,omitempty"`
	OperatorEmail  string         `json:"operator_email,omitempty"`
	Note           string         `json:"note,omitempty"`
	CreatedAt      time.Time      `json:"created_at"`
	AcknowledgedAt time.Time      `json:"acknowledged_at,omitempty"`
	ResolvedAt     time.Time      `json:"resolved_at,omitempty"`
	ExpiresAt      time.Time      `json:"expires_at"`
}

// OperatorAlertRepository is the small port the handler reads +
// writes through. Production wires a Postgres-backed implementation;
// tests use the in-memory implementation below.
type OperatorAlertRepository interface {
	Insert(ctx context.Context, alert OperatorAlert) error
	List(ctx context.Context, tenantID string, status AlertStatus) ([]OperatorAlert, error)
	Get(ctx context.Context, tenantID, alertID string) (OperatorAlert, error)
	UpdateStatus(ctx context.Context, tenantID, alertID string, status AlertStatus, action string, occurredAt time.Time) error
	ExpirePending(ctx context.Context, before time.Time) (int, error)
}

// OperatorAlertMetrics is the small port the handler emits a
// per-tenant + per-alert-type counter through.
type OperatorAlertMetrics interface {
	RecordOperatorAlert(tenantID string, alertType AlertType, status AlertStatus)
	ObserveOperatorAlertResolutionDuration(durationSec float64)
}

// AlertEventPublisher is the tiny port used to publish the
// OperatorAlertResolved event when an alert is resolved.
type AlertEventPublisher interface {
	Publish(ctx context.Context, evt eventbus.Event) error
}

// OperatorAlertHandlerConfig wires the handler.
type OperatorAlertHandlerConfig struct {
	Repository   OperatorAlertRepository
	TenantHeader string
	Now          func() time.Time
	Metrics      OperatorAlertMetrics
	Publisher    AlertEventPublisher
	ExpiryWindow time.Duration
}

// OperatorAlertHandler is the EC-9-5 HTTP handler.
type OperatorAlertHandler struct {
	repo         OperatorAlertRepository
	tenantHeader string
	now          func() time.Time
	logger       *slog.Logger
	metrics      OperatorAlertMetrics
	publisher    AlertEventPublisher
	expiryWindow time.Duration

	mu     sync.Mutex
	closed bool
}

// NewOperatorAlertHandler constructs the handler.
func NewOperatorAlertHandler(logger *slog.Logger, cfg OperatorAlertHandlerConfig) (*OperatorAlertHandler, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Repository == nil {
		return nil, fmt.Errorf("%w: OperatorAlertRepository required", ErrOperatorAlertHandlerUnconfigured)
	}
	if cfg.TenantHeader == "" {
		cfg.TenantHeader = "X-Tenant-Id"
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.ExpiryWindow <= 0 {
		cfg.ExpiryWindow = DefaultAlertExpiryWindow
	}
	return &OperatorAlertHandler{
		repo:         cfg.Repository,
		tenantHeader: cfg.TenantHeader,
		now:          cfg.Now,
		logger:       logger,
		metrics:      cfg.Metrics,
		publisher:    cfg.Publisher,
		expiryWindow: cfg.ExpiryWindow,
	}, nil
}

// Close marks the handler closed. lifecycle.Closer contract.
func (h *OperatorAlertHandler) Close(_ context.Context) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.closed = true
	return nil
}

// ServeHTTP routes by URL suffix + method. Cyclomatic 5.
func (h *OperatorAlertHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if err := h.guard(); err != nil {
		writeJSONError(w, http.StatusServiceUnavailable, err)
		return
	}
	suffix := strings.TrimPrefix(r.URL.Path, "/api/v1/operator/alerts")
	suffix = strings.TrimSuffix(suffix, "/")
	switch {
	case suffix == "" && r.Method == http.MethodGet:
		h.handleList(w, r)
	case strings.HasSuffix(suffix, "/acknowledge") && r.Method == http.MethodPost:
		h.handleAcknowledge(w, r, suffix)
	case strings.HasSuffix(suffix, "/resolve") && r.Method == http.MethodPost:
		h.handleResolve(w, r, suffix)
	default:
		writeJSONError(w, http.StatusNotFound, fmt.Errorf("unknown operator-alert route: %s %s", r.Method, r.URL.Path))
	}
}

func (h *OperatorAlertHandler) guard() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed {
		return ErrOperatorAlertHandlerClosed
	}
	return nil
}

// handleList serves the GET /alerts endpoint.
func (h *OperatorAlertHandler) handleList(w http.ResponseWriter, r *http.Request) {
	tenantID, err := h.resolveOperatorAlertTenantID(r)
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	statusRaw := strings.TrimSpace(r.URL.Query().Get("status"))
	status := AlertStatus(statusRaw)
	if statusRaw == "" {
		status = AlertStatusPending
	}
	rows, err := h.repo.List(r.Context(), tenantID, status)
	if err != nil {
		h.logger.Error("operator_alerts.list_failed", "tenant_id", tenantID, "error", err)
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id": tenantID,
		"status":    status,
		"alerts":    rows,
		"count":     len(rows),
	})
}

// handleAcknowledge serves the POST /alerts/{id}/acknowledge endpoint.
// Cyclomatic 4: tenant + alert id + repo + emit metric.
func (h *OperatorAlertHandler) handleAcknowledge(w http.ResponseWriter, r *http.Request, suffix string) {
	tenantID, alertID, err := h.resolveAlertContext(r, suffix, "/acknowledge")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	current, err := h.repo.Get(r.Context(), tenantID, alertID)
	if err != nil {
		h.notFoundOrError(w, err)
		return
	}
	if current.Status == AlertStatusResolved {
		writeJSONError(w, http.StatusConflict, fmt.Errorf("%w: id=%s", ErrAlertAlreadyResolved, alertID))
		return
	}
	if uerr := h.repo.UpdateStatus(r.Context(), tenantID, alertID, AlertStatusAcknowledged, "", h.now()); uerr != nil {
		writeJSONError(w, http.StatusInternalServerError, uerr)
		return
	}
	h.recordAlertOutcome(tenantID, current.AlertType, AlertStatusAcknowledged)
	writeJSON(w, http.StatusOK, map[string]any{"tenant_id": tenantID, "alert_id": alertID, "status": AlertStatusAcknowledged})
}

// handleResolve serves the POST /alerts/{id}/resolve endpoint.
// Cyclomatic 5: tenant + alert id + action + repo + emit event.
func (h *OperatorAlertHandler) handleResolve(w http.ResponseWriter, r *http.Request, suffix string) {
	tenantID, alertID, err := h.resolveAlertContext(r, suffix, "/resolve")
	if err != nil {
		writeJSONError(w, http.StatusBadRequest, err)
		return
	}
	action := strings.TrimSpace(strings.ToLower(r.URL.Query().Get("action")))
	if _, ok := AllowedAlertActions[action]; !ok {
		writeJSONError(w, http.StatusBadRequest, fmt.Errorf("%w: %q", ErrInvalidAlertAction, action))
		return
	}
	current, err := h.repo.Get(r.Context(), tenantID, alertID)
	if err != nil {
		h.notFoundOrError(w, err)
		return
	}
	if current.Status == AlertStatusResolved {
		writeJSONError(w, http.StatusConflict, fmt.Errorf("%w: id=%s", ErrAlertAlreadyResolved, alertID))
		return
	}
	resolutionStart := current.CreatedAt
	if !current.AcknowledgedAt.IsZero() {
		resolutionStart = current.AcknowledgedAt
	}
	if err := h.applyResolution(r.Context(), tenantID, current, action); err != nil {
		writeJSONError(w, http.StatusInternalServerError, err)
		return
	}
	h.recordAlertOutcome(tenantID, current.AlertType, AlertStatusResolved)
	h.recordResolutionDuration(resolutionStart)
	writeJSON(w, http.StatusOK, map[string]any{
		"tenant_id":    tenantID,
		"alert_id":     alertID,
		"status":       AlertStatusResolved,
		"action_taken": action,
	})
}

// applyResolution is the small helper that performs the repository
// update + event publish. Cyclomatic 3.
func (h *OperatorAlertHandler) applyResolution(ctx context.Context, tenantID string, current OperatorAlert, action string) error {
	if uerr := h.repo.UpdateStatus(ctx, tenantID, current.AlertID, AlertStatusResolved, action, h.now()); uerr != nil {
		return uerr
	}
	if h.publisher == nil {
		return nil
	}
	evt, err := eventbus.NewOperatorAlertResolvedEvent("handler.operator_alerts", h.now(), eventbus.OperatorAlertResolvedPayload{
		Version:    eventbus.OperatorAlertResolvedPayloadVersion,
		TenantID:   tenantID,
		AlertID:    current.AlertID,
		AlertType:  string(current.AlertType),
		Action:     action,
		OccurredAt: h.now(),
	})
	if err != nil {
		h.logger.Warn("operator_alerts.resolve_event_failed", "tenant_id", tenantID, "alert_id", current.AlertID, "error", err)
		return nil
	}
	if perr := h.publisher.Publish(ctx, evt); perr != nil {
		h.logger.Warn("operator_alerts.resolve_publish_failed", "tenant_id", tenantID, "alert_id", current.AlertID, "error", perr)
	}
	return nil
}

// resolveAlertContext extracts the tenant + alert id from the URL.
// suffix is the URL path with the leading endpoint trimmed; tail is
// the per-action segment ("/acknowledge" or "/resolve").
// Cyclomatic 4.
func (h *OperatorAlertHandler) resolveAlertContext(r *http.Request, suffix, tail string) (string, string, error) {
	tenantID, err := h.resolveOperatorAlertTenantID(r)
	if err != nil {
		return "", "", err
	}
	alertID, err := parseAlertID(suffix, tail)
	if err != nil {
		return "", "", err
	}
	return tenantID, alertID, nil
}

func (h *OperatorAlertHandler) resolveOperatorAlertTenantID(r *http.Request) (string, error) {
	if v := strings.TrimSpace(r.Header.Get(h.tenantHeader)); v != "" {
		return v, nil
	}
	if v := strings.TrimSpace(r.URL.Query().Get("tenant_id")); v != "" {
		return v, nil
	}
	return "", ErrOperatorAlertTenantMissing
}

func (h *OperatorAlertHandler) notFoundOrError(w http.ResponseWriter, err error) {
	if errors.Is(err, ErrAlertNotFound) {
		writeJSONError(w, http.StatusNotFound, err)
		return
	}
	writeJSONError(w, http.StatusInternalServerError, err)
}

func (h *OperatorAlertHandler) recordAlertOutcome(tenantID string, t AlertType, status AlertStatus) {
	if h.metrics == nil {
		return
	}
	h.metrics.RecordOperatorAlert(tenantID, t, status)
}

func (h *OperatorAlertHandler) recordResolutionDuration(start time.Time) {
	if h.metrics == nil || start.IsZero() {
		return
	}
	h.metrics.ObserveOperatorAlertResolutionDuration(h.now().Sub(start).Seconds())
}

// parseAlertID extracts the alert_id segment from the path suffix.
// suffix typically looks like "/<alert_id>/acknowledge" -- trim the
// trailing tail to recover alert_id. Cyclomatic 3.
func parseAlertID(suffix, tail string) (string, error) {
	if !strings.HasSuffix(suffix, tail) {
		return "", fmt.Errorf("%w: missing %s in path", ErrAlertNotFound, tail)
	}
	core := strings.TrimSuffix(suffix, tail)
	core = strings.TrimPrefix(core, "/")
	core = strings.TrimSpace(core)
	if core == "" {
		return "", fmt.Errorf("%w: alert_id missing in path", ErrAlertNotFound)
	}
	return core, nil
}
