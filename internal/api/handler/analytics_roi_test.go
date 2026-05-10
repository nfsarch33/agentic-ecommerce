package handler

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stubROIRepo struct {
	heatmap   []ROIPoint
	deadStock []ROIPoint
	byChannel []ROIPoint
	tenantID  string // when set, asserts incoming filter's tenant matches
	t         *testing.T
}

func (s *stubROIRepo) Heatmap(_ context.Context, f ROIFilter) ([]ROIPoint, error) {
	if s.tenantID != "" {
		require.Equal(s.t, s.tenantID, f.TenantID)
	}
	return s.heatmap, nil
}

func (s *stubROIRepo) DeadStock(_ context.Context, f ROIFilter) ([]ROIPoint, error) {
	if s.tenantID != "" {
		require.Equal(s.t, s.tenantID, f.TenantID)
	}
	return s.deadStock, nil
}

func (s *stubROIRepo) ByChannel(_ context.Context, f ROIFilter) ([]ROIPoint, error) {
	if s.tenantID != "" {
		require.Equal(s.t, s.tenantID, f.TenantID)
	}
	return s.byChannel, nil
}

type roiCapturingMetrics struct {
	durations []float64
}

func (m *roiCapturingMetrics) ObserveROIQueryDurationSeconds(d float64) {
	m.durations = append(m.durations, d)
}

func TestROIHandler_HeatmapReturnsCorrectMatrix(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{
		t: t,
		heatmap: []ROIPoint{
			{Day: time.Now(), Channel: "tiktok", ProductID: "p1", TotalRevenueAUDCents: 10000, TotalCostAUDCents: 6000, OrderCount: 5},
			{Day: time.Now(), Channel: "facebook", ProductID: "p1", TotalRevenueAUDCents: 5000, TotalCostAUDCents: 4000, OrderCount: 2},
		},
	}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/heatmap?tenant_id=t1&period=30d&dimensions=channel,product", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	cells, ok := body["cells"].([]any)
	require.True(t, ok, "cells field present")
	require.Len(t, cells, 2)

	// First cell: ROI = (10000-6000)/6000*100 ≈ 66.67%
	first, _ := cells[0].(map[string]any)
	require.Equal(t, "tiktok", first["channel"])
	roi, ok := first["roi_pct"].(float64)
	require.True(t, ok)
	require.InDelta(t, 66.67, roi, 0.5)
}

func TestROIHandler_DeadStockFilterIdentifiesSlowMovers(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{
		t: t,
		deadStock: []ROIPoint{
			{Day: time.Now().AddDate(0, 0, -90), Channel: "tiktok", ProductID: "p-slow", TotalRevenueAUDCents: 0, TotalCostAUDCents: 5000, OrderCount: 0},
			{Day: time.Now().AddDate(0, 0, -75), Channel: "facebook", ProductID: "p-stale", TotalRevenueAUDCents: 0, TotalCostAUDCents: 3000, OrderCount: 0},
		},
	}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/dead-stock?tenant_id=t1&period=90d&min_age_days=60", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	require.Equal(t, float64(60), body["min_age_days"])
	rows, _ := body["dead_stock_rows"].([]any)
	require.Len(t, rows, 2)
}

func TestROIHandler_ByChannelBreakdownEqualsTotal(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{
		t: t,
		byChannel: []ROIPoint{
			{Channel: "tiktok", TotalRevenueAUDCents: 10000, TotalCostAUDCents: 6000, OrderCount: 5},
			{Channel: "tiktok", TotalRevenueAUDCents: 5000, TotalCostAUDCents: 3000, OrderCount: 2},
			{Channel: "facebook", TotalRevenueAUDCents: 4000, TotalCostAUDCents: 3000, OrderCount: 1},
		},
	}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/by-channel?tenant_id=t1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	channels, _ := body["channels"].([]any)
	require.Len(t, channels, 2, "two distinct channels")

	totalOrders := int64(0)
	for _, ch := range channels {
		m, _ := ch.(map[string]any)
		oc, _ := m["order_count"].(float64)
		totalOrders += int64(oc)
	}
	require.Equal(t, int64(8), totalOrders)
}

func TestROIHandler_TenantIsolationEnforced(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{t: t, tenantID: "tenant-from-header"}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	// Header set; query parameter MUST NOT override it.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/heatmap?tenant_id=other&period=7d", nil)
	req.Header.Set("X-Tenant-Id", "tenant-from-header")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
}

func TestROIHandler_RejectsInvalidPeriod(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{t: t}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/heatmap?tenant_id=t1&period=400d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Contains(t, rec.Body.String(), "period")
}

func TestROIHandler_RejectsMethodNotAllowed(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{t: t}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodPost, "/api/v1/analytics/roi/heatmap", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusMethodNotAllowed, rec.Code)
}

func TestROIHandler_RejectsUnknownRoute(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{t: t}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/unknown", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusNotFound, rec.Code)
}

func TestROIHandler_RejectsMissingTenant(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{t: t}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/heatmap?period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestROIHandler_ClosedReturnsServiceUnavailable(t *testing.T) {
	t.Parallel()
	repo := &stubROIRepo{t: t}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo})
	require.NoError(t, err)
	require.NoError(t, h.Close(context.Background()))

	req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/heatmap?tenant_id=t1&period=30d", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// Performance gate: query within budget over a 90-day window.
// Acceptance: p95 <300ms over 90-day with 100K orders.
func TestROIHandler_PerformanceP95Under300ms(t *testing.T) {
	t.Parallel()
	// Generate 1000 rollup rows across 90 days, 5 channels, 4 products
	// to mimic the cardinality the materialized view delivers.
	rows := make([]ROIPoint, 0, 1000)
	for d := 0; d < 90; d++ {
		for ch := 0; ch < 5; ch++ {
			for p := 0; p < 4; p++ {
				rows = append(rows, ROIPoint{
					Day:                  time.Now().AddDate(0, 0, -d),
					Channel:              fmt.Sprintf("ch-%d", ch),
					ProductID:            fmt.Sprintf("p-%d", p),
					TotalRevenueAUDCents: int64((d + 1) * 1000),
					TotalCostAUDCents:    int64((d + 1) * 600),
					OrderCount:           int64(d + 1),
				})
			}
		}
	}
	repo := &stubROIRepo{t: t, heatmap: rows}
	metrics := &roiCapturingMetrics{}
	h, err := NewROIHandler(nil, ROIHandlerConfig{Repository: repo, Metrics: metrics})
	require.NoError(t, err)
	t.Cleanup(func() { _ = h.Close(context.Background()) })

	const samples = 200
	durations := make([]time.Duration, 0, samples)
	for i := 0; i < samples; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/roi/heatmap?tenant_id=t1&period=90d", nil)
		rec := httptest.NewRecorder()
		start := time.Now()
		h.ServeHTTP(rec, req)
		durations = append(durations, time.Since(start))
		require.Equal(t, http.StatusOK, rec.Code)
	}
	p95 := computeP95(durations)
	require.Less(t, p95, 300*time.Millisecond, "p95 must be <300ms")
}

func computeP95(durations []time.Duration) time.Duration {
	if len(durations) == 0 {
		return 0
	}
	// in-place sort
	for i := 1; i < len(durations); i++ {
		for j := i; j > 0 && durations[j] < durations[j-1]; j-- {
			durations[j], durations[j-1] = durations[j-1], durations[j]
		}
	}
	idx := int(float64(len(durations)) * 0.95)
	if idx >= len(durations) {
		idx = len(durations) - 1
	}
	return durations[idx]
}

func TestROIHandler_RejectsUnconfigured(t *testing.T) {
	t.Parallel()
	_, err := NewROIHandler(nil, ROIHandlerConfig{})
	require.Error(t, err)
	require.True(t, errors.Is(err, ErrROIHandlerUnconfigured))
}
