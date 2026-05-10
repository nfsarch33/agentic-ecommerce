// File scope: v3.9.0 EC-6-5 margin dashboard handler RED tests.
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
)

type stubMarginRepo struct {
	mu        sync.Mutex
	dashboard MarginDashboardSnapshot
	alerts    []MarginAlert
	forecast  MarginForecast
	err       error
	calls     int
}

func (r *stubMarginRepo) Dashboard(_ context.Context, _ MarginFilter) (MarginDashboardSnapshot, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return MarginDashboardSnapshot{}, r.err
	}
	return r.dashboard, nil
}

func (r *stubMarginRepo) Alerts(_ context.Context, _ MarginFilter) ([]MarginAlert, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return nil, r.err
	}
	return r.alerts, nil
}

func (r *stubMarginRepo) Forecast(_ context.Context, _ MarginFilter) (MarginForecast, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.calls++
	if r.err != nil {
		return MarginForecast{}, r.err
	}
	return r.forecast, nil
}

func newMarginHandlerHarness(t *testing.T, repo *stubMarginRepo) *MarginHandler {
	t.Helper()
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	h, err := NewMarginHandler(nil, MarginHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return clk },
	})
	if err != nil {
		t.Fatalf("NewMarginHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	return h
}

func TestMarginHandler_DashboardEndpoint(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{
		dashboard: MarginDashboardSnapshot{
			RevenueAUDCents:       150_00_00,
			SupplierCostAUDCents:  60_00_00,
			ShippingCostAUDCents:  10_00_00,
			NetMarginAUDCents:     80_00_00,
			NetMarginPct:          0.5333,
			ROIPct:                1.0,
			OrderCount:            120,
			CompetitorAvgAUDCents: 14_50_00,
			CompetitorPositioning: "above",
		},
	}
	h := newMarginHandlerHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/dashboard?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if out["tenant_id"] != "tenant-1" {
		t.Fatalf("expected tenant_id tenant-1, got %v", out["tenant_id"])
	}
	if _, ok := out["dashboard"]; !ok {
		t.Fatalf("expected dashboard field in response: %v", out)
	}
}

func TestMarginHandler_AlertsEndpoint(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{
		alerts: []MarginAlert{
			{ProductID: "sku-1", Severity: "warning", Reason: "near_floor", DeltaPct: -0.04},
			{ProductID: "sku-2", Severity: "critical", Reason: "competitor_undercut", DeltaPct: -0.10},
		},
	}
	h := newMarginHandlerHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/alerts?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "competitor_undercut") {
		t.Fatalf("expected critical alert in body: %s", rec.Body.String())
	}
}

func TestMarginHandler_ForecastEndpoint(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{
		forecast: MarginForecast{
			ForecastAUDCents:   200_00_00,
			LowerBoundAUDCents: 180_00_00,
			UpperBoundAUDCents: 220_00_00,
			ConfidencePct:      0.85,
			BasedOnDays:        30,
		},
	}
	h := newMarginHandlerHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/forecast?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "forecast_aud_cents") {
		t.Fatalf("expected forecast_aud_cents in body: %s", rec.Body.String())
	}
}

func TestMarginHandler_TenantMissingReturns400(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{}
	h := newMarginHandlerHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/dashboard?period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMarginHandler_InvalidPeriodReturns400(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{}
	h := newMarginHandlerHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/dashboard?tenant_id=tenant-1&period=42d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMarginHandler_RepositoryErrorReturns500(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{err: errors.New("db unreachable")}
	h := newMarginHandlerHarness(t, repo)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/dashboard?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d body=%s", rec.Code, rec.Body.String())
	}
}

func TestMarginHandler_MethodNotAllowed(t *testing.T) {
	t.Parallel()
	h := newMarginHandlerHarness(t, &stubMarginRepo{})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/margin/dashboard", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestMarginHandler_UnknownRouteReturns404(t *testing.T) {
	t.Parallel()
	h := newMarginHandlerHarness(t, &stubMarginRepo{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/unknown?tenant_id=tenant-1", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestMarginHandler_ClosedReturns503(t *testing.T) {
	t.Parallel()
	h := newMarginHandlerHarness(t, &stubMarginRepo{})
	if err := h.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/dashboard?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestMarginHandler_MetricsObserved(t *testing.T) {
	t.Parallel()
	repo := &stubMarginRepo{}
	var samples []float64
	clk := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	h, err := NewMarginHandler(nil, MarginHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return clk },
		Metrics:    stubMarginMetrics{onObserve: func(d float64) { samples = append(samples, d) }},
	})
	if err != nil {
		t.Fatalf("NewMarginHandler: %v", err)
	}
	t.Cleanup(func() { _ = h.Close(context.Background()) })
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/margin/dashboard?tenant_id=tenant-1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
	if len(samples) != 1 {
		t.Fatalf("expected 1 metric observation, got %d", len(samples))
	}
}

type stubMarginMetrics struct {
	onObserve func(float64)
}

func (s stubMarginMetrics) ObserveMarginDashboardDuration(durationSec float64) {
	if s.onObserve != nil {
		s.onObserve(durationSec)
	}
}
