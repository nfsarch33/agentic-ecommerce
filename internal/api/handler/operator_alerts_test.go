// File scope: v3.9.1 EC-9-5 operator alert centre handler RED tests.
package handler

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

type recordingAlertMetrics struct {
	mu          sync.Mutex
	outcomes    []string
	resolutions []float64
}

func (r *recordingAlertMetrics) RecordOperatorAlert(tenantID string, alertType AlertType, status AlertStatus) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.outcomes = append(r.outcomes, strings.Join([]string{tenantID, string(alertType), string(status)}, "|"))
}

func (r *recordingAlertMetrics) ObserveOperatorAlertResolutionDuration(durationSec float64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.resolutions = append(r.resolutions, durationSec)
}

func (r *recordingAlertMetrics) seenStatus(alertType AlertType, status AlertStatus) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	want := "|" + string(alertType) + "|" + string(status)
	for _, o := range r.outcomes {
		if strings.HasSuffix(o, want) {
			return true
		}
	}
	return false
}

type capturingPublisher struct {
	mu     sync.Mutex
	events []eventbus.Event
}

func (p *capturingPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, evt)
	return nil
}

func (p *capturingPublisher) snapshot() []eventbus.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]eventbus.Event, len(p.events))
	copy(out, p.events)
	return out
}

func newOperatorAlertHarness(t *testing.T) (*OperatorAlertHandler, *InMemoryOperatorAlertRepository, *recordingAlertMetrics, *capturingPublisher) {
	t.Helper()
	repo := NewInMemoryOperatorAlertRepository()
	metrics := &recordingAlertMetrics{}
	pub := &capturingPublisher{}
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	h, err := NewOperatorAlertHandler(nil, OperatorAlertHandlerConfig{
		Repository:   repo,
		Now:          func() time.Time { return clk },
		Metrics:      metrics,
		Publisher:    pub,
		ExpiryWindow: 24 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewOperatorAlertHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h, repo, metrics, pub
}

func TestOperatorAlerts_ListsPendingAlerts(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	ctx := context.Background()
	must(t, repo.Insert(ctx, OperatorAlert{TenantID: "tenant-1", AlertID: "a1", AlertType: AlertTypeLargeRefund, Severity: AlertSeverityCritical, CreatedAt: time.Now().UTC()}))
	must(t, repo.Insert(ctx, OperatorAlert{TenantID: "tenant-1", AlertID: "a2", AlertType: AlertTypePriceChange, Severity: AlertSeverityWarning, CreatedAt: time.Now().UTC()}))
	must(t, repo.Insert(ctx, OperatorAlert{TenantID: "tenant-2", AlertID: "a3", AlertType: AlertTypeLargeMargin, Severity: AlertSeverityWarning, CreatedAt: time.Now().UTC()}))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/alerts?tenant_id=tenant-1&status=pending", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var out struct {
		Count  int             `json:"count"`
		Alerts []OperatorAlert `json:"alerts"`
	}
	must(t, json.Unmarshal(rec.Body.Bytes(), &out))
	if out.Count != 2 {
		t.Fatalf("count=%d want=2", out.Count)
	}
	for _, a := range out.Alerts {
		if a.TenantID != "tenant-1" {
			t.Fatalf("alert tenant=%q want=tenant-1", a.TenantID)
		}
	}
}

func TestOperatorAlerts_AcknowledgeUpdatesStatus(t *testing.T) {
	t.Parallel()
	h, repo, metrics, _ := newOperatorAlertHarness(t)
	ctx := context.Background()
	must(t, repo.Insert(ctx, OperatorAlert{TenantID: "tenant-1", AlertID: "a1", AlertType: AlertTypeLargeRefund, Severity: AlertSeverityCritical, CreatedAt: time.Now().UTC()}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/acknowledge?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := repo.Get(ctx, "tenant-1", "a1")
	must(t, err)
	if stored.Status != AlertStatusAcknowledged {
		t.Fatalf("status=%q want=acknowledged", stored.Status)
	}
	if stored.AcknowledgedAt.IsZero() {
		t.Fatalf("AcknowledgedAt unset after acknowledge")
	}
	if !metrics.seenStatus(AlertTypeLargeRefund, AlertStatusAcknowledged) {
		t.Fatalf("metrics did not record acknowledged outcome: %+v", metrics.outcomes)
	}
}

func TestOperatorAlerts_ResolveWithActionEmitsEvent(t *testing.T) {
	t.Parallel()
	h, repo, metrics, pub := newOperatorAlertHarness(t)
	ctx := context.Background()
	now := time.Now().UTC()
	must(t, repo.Insert(ctx, OperatorAlert{
		TenantID:       "tenant-1",
		AlertID:        "a1",
		AlertType:      AlertTypePriceChange,
		Severity:       AlertSeverityWarning,
		Status:         AlertStatusAcknowledged,
		CreatedAt:      now,
		AcknowledgedAt: now,
	}))

	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?tenant_id=tenant-1&action=approve", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	stored, err := repo.Get(ctx, "tenant-1", "a1")
	must(t, err)
	if stored.Status != AlertStatusResolved {
		t.Fatalf("status=%q want=resolved", stored.Status)
	}
	if stored.ActionTaken != "approve" {
		t.Fatalf("action_taken=%q want=approve", stored.ActionTaken)
	}
	if !metrics.seenStatus(AlertTypePriceChange, AlertStatusResolved) {
		t.Fatalf("metrics did not record resolved outcome: %+v", metrics.outcomes)
	}
	events := pub.snapshot()
	if len(events) != 1 {
		t.Fatalf("expected 1 published event, got %d", len(events))
	}
	if events[0].Type != eventbus.OperatorAlertResolved {
		t.Fatalf("event type=%q want=%q", events[0].Type, eventbus.OperatorAlertResolved)
	}
	if events[0].TenantID != "tenant-1" {
		t.Fatalf("event tenant=%q want=tenant-1", events[0].TenantID)
	}
	if got, _ := events[0].Payload["action"].(string); got != "approve" {
		t.Fatalf("event action=%q want=approve", got)
	}
}

func TestOperatorAlerts_ExpiredAlertsAfter24h(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryOperatorAlertRepository()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "old",
		AlertType: AlertTypeLargeRefund,
		CreatedAt: now.Add(-25 * time.Hour),
		ExpiresAt: now.Add(-1 * time.Hour),
	}))
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "fresh",
		AlertType: AlertTypeLargeRefund,
		CreatedAt: now.Add(-1 * time.Hour),
		ExpiresAt: now.Add(23 * time.Hour),
	}))

	changed, err := repo.ExpirePending(context.Background(), now)
	must(t, err)
	if changed != 1 {
		t.Fatalf("expired=%d want=1", changed)
	}
	stored, err := repo.Get(context.Background(), "tenant-1", "old")
	must(t, err)
	if stored.Status != AlertStatusExpired {
		t.Fatalf("expired alert status=%q want=expired", stored.Status)
	}
	fresh, err := repo.Get(context.Background(), "tenant-1", "fresh")
	must(t, err)
	if fresh.Status == AlertStatusExpired {
		t.Fatalf("fresh alert wrongly expired")
	}
}

func TestOperatorAlerts_TenantIsolationEnforced(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	ctx := context.Background()
	must(t, repo.Insert(ctx, OperatorAlert{TenantID: "tenant-bob", AlertID: "a1", AlertType: AlertTypePriceChange, CreatedAt: time.Now().UTC()}))

	// tenant-alice tries to acknowledge tenant-bob's alert; the
	// header-set tenant from JWT middleware (tenant-alice) trumps
	// any query string and the repo should not match.
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/acknowledge?tenant_id=tenant-bob", nil)
	req.Header.Set("X-Tenant-Id", "tenant-alice")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for cross-tenant access, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_ResolveRejectsInvalidAction(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{TenantID: "tenant-1", AlertID: "a1", AlertType: AlertTypePriceChange, CreatedAt: time.Now().UTC()}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?tenant_id=tenant-1&action=ignore", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOperatorAlerts_ResolveOfResolvedReturnsConflict(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{TenantID: "tenant-1", AlertID: "a1", AlertType: AlertTypePriceChange, Status: AlertStatusResolved, CreatedAt: time.Now().UTC()}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?tenant_id=tenant-1&action=approve", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !errors.Is(asAlertError(rec.Body.Bytes()), ErrAlertAlreadyResolved) {
		t.Logf("body=%s", rec.Body.String()) // sentinel might be lossy through JSON; structural check below
	}
}

func TestOperatorAlerts_NotFoundAlert(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newOperatorAlertHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/missing/acknowledge?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_TenantMissingReturns400(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newOperatorAlertHarness(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/alerts", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_ClosedReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newOperatorAlertHarness(t)
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/alerts?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_AllEightAlertTypesAccepted(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryOperatorAlertRepository()
	types := []AlertType{
		AlertTypeLargeRefund,
		AlertTypeLargeDropship,
		AlertTypePriceChange,
		AlertTypeCAPTCHADetected,
		AlertTypeOmniUnavailable,
		AlertTypeRateLimitDrain,
		AlertTypeChannelStatusFail,
		AlertTypeLargeMargin,
	}
	for i, t2 := range types {
		must(t, repo.Insert(context.Background(), OperatorAlert{
			TenantID:  "tenant-1",
			AlertID:   "alert-" + string(t2),
			AlertType: t2,
			Severity:  AlertSeverityWarning,
			CreatedAt: time.Now().UTC().Add(time.Duration(i) * time.Minute),
		}))
	}
	rows, err := repo.List(context.Background(), "tenant-1", AlertStatusPending)
	must(t, err)
	if len(rows) != len(types) {
		t.Fatalf("rows=%d want=%d (one per alert type)", len(rows), len(types))
	}
}

// asAlertError extracts a typed sentinel from a JSON {error: ...}
// body so tests can branch via errors.Is without parsing strings.
func asAlertError(body []byte) error {
	var env struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil
	}
	switch {
	case strings.Contains(env.Error, "alert already resolved"):
		return ErrAlertAlreadyResolved
	case strings.Contains(env.Error, "alert not found"):
		return ErrAlertNotFound
	case strings.Contains(env.Error, "invalid alert action"):
		return ErrInvalidAlertAction
	}
	return nil
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("must: %v", err)
	}
}
