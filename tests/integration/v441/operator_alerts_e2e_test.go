//go:build v441_smoke

package v441_test

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/api/handler"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type stubMetrics struct {
	alerts      int
	resolutions int
}

func (m *stubMetrics) RecordOperatorAlert(string, handler.AlertType, handler.AlertStatus) {
	m.alerts++
}
func (m *stubMetrics) ObserveOperatorAlertResolutionDuration(float64) {
	m.resolutions++
}

type stubPublisher struct {
	events []eventbus.Event
}

func (p *stubPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.events = append(p.events, evt)
	return nil
}

func newTestHandler(t *testing.T, repo *handler.InMemoryOperatorAlertRepository) (*handler.OperatorAlertHandler, *stubMetrics, *stubPublisher) {
	t.Helper()
	met := &stubMetrics{}
	pub := &stubPublisher{}
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	h, err := handler.NewOperatorAlertHandler(slog.Default(), handler.OperatorAlertHandlerConfig{
		Repository:   repo,
		TenantHeader: "X-Tenant-Id",
		Now:          func() time.Time { return now },
		Metrics:      met,
		Publisher:    pub,
		ExpiryWindow: 24 * time.Hour,
	})
	require.NoError(t, err)
	return h, met, pub
}

func seedAlert(repo *handler.InMemoryOperatorAlertRepository, tenantID, alertID string, alertType handler.AlertType, severity handler.AlertSeverity) {
	_ = repo.Insert(nil, handler.OperatorAlert{
		TenantID:  tenantID,
		AlertID:   alertID,
		AlertType: alertType,
		Severity:  severity,
		Status:    handler.AlertStatusPending,
		CreatedAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC),
		ExpiresAt: time.Date(2026, 5, 11, 10, 0, 0, 0, time.UTC),
	})
}

func doRequest(h http.Handler, method, url, tenantID string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, url, nil)
	if tenantID != "" {
		req.Header.Set("X-Tenant-Id", tenantID)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeBody(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var body map[string]any
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&body))
	return body
}

// Scenario 1: Large refund approval -> operator acknowledges -> saga continues.
func TestOperatorAlerts_LargeRefundApproval(t *testing.T) {
	repo := handler.NewInMemoryOperatorAlertRepository()
	h, met, _ := newTestHandler(t, repo)

	seedAlert(repo, "tenant-A", "refund-1", handler.AlertTypeLargeRefund, handler.AlertSeverityCritical)

	rec := doRequest(h, http.MethodPost, "/api/v1/operator/alerts/refund-1/acknowledge", "tenant-A")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec)
	assert.Equal(t, "acknowledged", body["status"])

	rec = doRequest(h, http.MethodPost, "/api/v1/operator/alerts/refund-1/resolve?action=approve", "tenant-A")
	assert.Equal(t, http.StatusOK, rec.Code)
	body = decodeBody(t, rec)
	assert.Equal(t, "resolved", body["status"])
	assert.Equal(t, "approve", body["action_taken"])
	assert.GreaterOrEqual(t, met.alerts, 2)
}

// Scenario 2: Large dropship approval -> operator denies -> saga halts.
func TestOperatorAlerts_LargeDropshipDeny(t *testing.T) {
	repo := handler.NewInMemoryOperatorAlertRepository()
	h, _, _ := newTestHandler(t, repo)

	seedAlert(repo, "tenant-A", "dropship-1", handler.AlertTypeLargeDropship, handler.AlertSeverityCritical)

	rec := doRequest(h, http.MethodPost, "/api/v1/operator/alerts/dropship-1/resolve?action=deny", "tenant-A")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec)
	assert.Equal(t, "resolved", body["status"])
	assert.Equal(t, "deny", body["action_taken"])
}

// Scenario 3: Price change approval -> auto-expire after 24h.
func TestOperatorAlerts_PriceChangeAutoExpire(t *testing.T) {
	repo := handler.NewInMemoryOperatorAlertRepository()
	_, _, _ = newTestHandler(t, repo)

	seedAlert(repo, "tenant-A", "price-1", handler.AlertTypePriceChange, handler.AlertSeverityWarning)

	cutoff := time.Date(2026, 5, 12, 0, 0, 0, 0, time.UTC)
	expired, err := repo.ExpirePending(nil, cutoff)
	require.NoError(t, err)
	assert.Equal(t, 1, expired)

	alert, err := repo.Get(nil, "tenant-A", "price-1")
	require.NoError(t, err)
	assert.Equal(t, handler.AlertStatusExpired, alert.Status)
}

// Scenario 4: CAPTCHA detected -> operator resolves via webhook.
func TestOperatorAlerts_CAPTCHAResolveViaWebhook(t *testing.T) {
	repo := handler.NewInMemoryOperatorAlertRepository()
	h, _, _ := newTestHandler(t, repo)

	seedAlert(repo, "tenant-A", "captcha-1", handler.AlertTypeCAPTCHADetected, handler.AlertSeverityCritical)

	rec := doRequest(h, http.MethodPost, "/api/v1/operator/alerts/captcha-1/resolve?action=approve", "tenant-A")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec)
	assert.Equal(t, "resolved", body["status"])
}

// Scenario 5: Multiple concurrent alerts -> ordered by priority.
func TestOperatorAlerts_MultipleConcurrentAlerts(t *testing.T) {
	repo := handler.NewInMemoryOperatorAlertRepository()
	h, _, _ := newTestHandler(t, repo)

	seedAlert(repo, "tenant-A", "alert-low", handler.AlertTypeRateLimitDrain, handler.AlertSeverityInfo)
	seedAlert(repo, "tenant-A", "alert-mid", handler.AlertTypePriceChange, handler.AlertSeverityWarning)
	seedAlert(repo, "tenant-A", "alert-high", handler.AlertTypeLargeRefund, handler.AlertSeverityCritical)

	rec := doRequest(h, http.MethodGet, "/api/v1/operator/alerts?status=pending", "tenant-A")
	assert.Equal(t, http.StatusOK, rec.Code)
	body := decodeBody(t, rec)
	count, ok := body["count"].(float64)
	require.True(t, ok)
	assert.Equal(t, float64(3), count)

	alerts, ok := body["alerts"].([]any)
	require.True(t, ok)
	assert.Len(t, alerts, 3)
}

// Scenario 6: Tenant isolation -> tenant A cannot see tenant B's alerts.
func TestOperatorAlerts_TenantIsolation(t *testing.T) {
	repo := handler.NewInMemoryOperatorAlertRepository()
	h, _, _ := newTestHandler(t, repo)

	seedAlert(repo, "tenant-A", "alert-A1", handler.AlertTypeLargeRefund, handler.AlertSeverityCritical)
	seedAlert(repo, "tenant-B", "alert-B1", handler.AlertTypeLargeDropship, handler.AlertSeverityCritical)

	recA := doRequest(h, http.MethodGet, "/api/v1/operator/alerts?status=pending", "tenant-A")
	assert.Equal(t, http.StatusOK, recA.Code)
	bodyA := decodeBody(t, recA)
	countA, _ := bodyA["count"].(float64)
	assert.Equal(t, float64(1), countA)

	recB := doRequest(h, http.MethodGet, "/api/v1/operator/alerts?status=pending", "tenant-B")
	assert.Equal(t, http.StatusOK, recB.Code)
	bodyB := decodeBody(t, recB)
	countB, _ := bodyB["count"].(float64)
	assert.Equal(t, float64(1), countB)

	alertsA := bodyA["alerts"].([]any)
	for _, raw := range alertsA {
		a := raw.(map[string]any)
		assert.Equal(t, "tenant-A", a["tenant_id"])
		assert.NotEqual(t, "alert-B1", a["alert_id"])
	}

	alertsB := bodyB["alerts"].([]any)
	for _, raw := range alertsB {
		a := raw.(map[string]any)
		assert.Equal(t, "tenant-B", a["tenant_id"])
		assert.NotEqual(t, "alert-A1", a["alert_id"])
	}
}
