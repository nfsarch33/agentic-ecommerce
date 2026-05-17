// File scope: v3.9.1 EC-9-5 -- additional operator alert centre
// handler coverage tests (acknowledge/resolve error paths).
package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestOperatorAlerts_AcknowledgeMissingTenant(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newOperatorAlertHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/acknowledge", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOperatorAlerts_ResolveMissingTenant(t *testing.T) {
	t.Parallel()
	h, _, _, _ := newOperatorAlertHarness(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?action=approve", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestOperatorAlerts_ListAcknowledgedStatus(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "a1",
		AlertType: AlertTypePriceChange,
		Status:    AlertStatusAcknowledged,
		CreatedAt: time.Now().UTC(),
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/alerts?tenant_id=tenant-1&status=acknowledged", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_AcknowledgeAlreadyResolvedReturnsConflict(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "a1",
		AlertType: AlertTypePriceChange,
		Status:    AlertStatusResolved,
		CreatedAt: time.Now().UTC(),
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/acknowledge?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_AcknowledgeAlreadyAcknowledgedReturnsConflict(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:       "tenant-1",
		AlertID:        "a1",
		AlertType:      AlertTypePriceChange,
		Status:         AlertStatusAcknowledged,
		CreatedAt:      time.Now().UTC(),
		AcknowledgedAt: time.Now().UTC(),
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/acknowledge?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for repeated acknowledge, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_ResolveMissingActionParam(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "a1",
		AlertType: AlertTypePriceChange,
		Status:    AlertStatusPending,
		CreatedAt: time.Now().UTC(),
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing action, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_ResolvePendingReturnsConflictUntilAcknowledged(t *testing.T) {
	t.Parallel()
	h, repo, _, _ := newOperatorAlertHarness(t)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "a1",
		AlertType: AlertTypePriceChange,
		Status:    AlertStatusPending,
		CreatedAt: time.Now().UTC(),
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?tenant_id=tenant-1&action=approve", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 for resolve-before-acknowledge, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestOperatorAlerts_ResolveAcknowledgedAlertMeasuresFromAck(t *testing.T) {
	t.Parallel()
	h, repo, metrics, _ := newOperatorAlertHarness(t)
	created := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	ackd := created.Add(2 * time.Minute)
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:       "tenant-1",
		AlertID:        "a1",
		AlertType:      AlertTypePriceChange,
		Status:         AlertStatusAcknowledged,
		CreatedAt:      created,
		AcknowledgedAt: ackd,
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/resolve?tenant_id=tenant-1&action=approve", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(metrics.resolutions) != 1 {
		t.Fatalf("expected 1 resolution duration sample, got %d", len(metrics.resolutions))
	}
}

func TestOperatorAlerts_NoMetricsConfig(t *testing.T) {
	t.Parallel()
	repo := NewInMemoryOperatorAlertRepository()
	h, err := NewOperatorAlertHandler(nil, OperatorAlertHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewOperatorAlertHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	must(t, repo.Insert(context.Background(), OperatorAlert{
		TenantID:  "tenant-1",
		AlertID:   "a1",
		AlertType: AlertTypePriceChange,
		Status:    AlertStatusPending,
		CreatedAt: time.Now().UTC(),
	}))
	req := httptest.NewRequest(http.MethodPost, "/api/v1/operator/alerts/a1/acknowledge?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with no metrics config, got %d", rec.Code)
	}
}
