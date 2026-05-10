package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type inMemoryDashboardRepo struct {
	orders   int64
	gmvToday int64
	gmvMTD   int64
	alerts   int
	channels int
	health   []ChannelHealthEntry
	actions  []RecentAgentAction
}

func (r *inMemoryDashboardRepo) ActiveOrders(_ context.Context, _ string) (int64, error) {
	return r.orders, nil
}
func (r *inMemoryDashboardRepo) GMVToday(_ context.Context, _ string) (int64, error) {
	return r.gmvToday, nil
}
func (r *inMemoryDashboardRepo) GMVMTD(_ context.Context, _ string) (int64, error) {
	return r.gmvMTD, nil
}
func (r *inMemoryDashboardRepo) PendingAlerts(_ context.Context, _ string) (int, error) {
	return r.alerts, nil
}
func (r *inMemoryDashboardRepo) ActiveChannels(_ context.Context, _ string) (int, error) {
	return r.channels, nil
}
func (r *inMemoryDashboardRepo) ChannelHealth(_ context.Context, _ string) ([]ChannelHealthEntry, error) {
	return r.health, nil
}
func (r *inMemoryDashboardRepo) RecentAgentActions(_ context.Context, _ string, _ int) ([]RecentAgentAction, error) {
	return r.actions, nil
}

type recordingDashboardMetrics struct {
	durations []float64
}

func (m *recordingDashboardMetrics) ObserveTenantDashboardDuration(d float64) {
	m.durations = append(m.durations, d)
}

func newDashboardHarness(t *testing.T) (*TenantDashboardHandler, *inMemoryDashboardRepo, *recordingDashboardMetrics) {
	t.Helper()
	repo := &inMemoryDashboardRepo{
		orders:   42,
		gmvToday: 150000,
		gmvMTD:   2500000,
		alerts:   3,
		channels: 4,
		health: []ChannelHealthEntry{
			{Channel: "tiktok", Status: "healthy"},
			{Channel: "facebook", Status: "degraded"},
		},
		actions: []RecentAgentAction{
			{AgentID: "pricing", Action: "price.change.applied", Status: "applied", Timestamp: time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)},
		},
	}
	metrics := &recordingDashboardMetrics{}
	h, err := NewTenantDashboardHandler(nil, TenantDashboardHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
		Metrics:    metrics,
	})
	if err != nil {
		t.Fatalf("NewTenantDashboardHandler: %v", err)
	}
	return h, repo, metrics
}

func TestTenantDashboard_FullRender(t *testing.T) {
	t.Parallel()
	h, _, metrics := newDashboardHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t1/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var kpis TenantDashboardKPIs
	if err := json.NewDecoder(w.Body).Decode(&kpis); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if kpis.TenantID != "t1" {
		t.Fatalf("tenant_id = %s, want t1", kpis.TenantID)
	}
	if kpis.ActiveOrders != 42 {
		t.Fatalf("active_orders = %d, want 42", kpis.ActiveOrders)
	}
	if kpis.GMVTodayCents != 150000 {
		t.Fatalf("gmv_today = %d, want 150000", kpis.GMVTodayCents)
	}
	if kpis.GMVMTDCents != 2500000 {
		t.Fatalf("gmv_mtd = %d, want 2500000", kpis.GMVMTDCents)
	}
	if kpis.PendingAlerts != 3 {
		t.Fatalf("pending_alerts = %d, want 3", kpis.PendingAlerts)
	}
	if kpis.ActiveChannels != 4 {
		t.Fatalf("active_channels = %d, want 4", kpis.ActiveChannels)
	}
	if len(kpis.ChannelHealthSummary) != 2 {
		t.Fatalf("channel_health = %d entries, want 2", len(kpis.ChannelHealthSummary))
	}
	if len(kpis.RecentAgentActions) != 1 {
		t.Fatalf("recent_actions = %d, want 1", len(kpis.RecentAgentActions))
	}
	if len(metrics.durations) != 1 {
		t.Fatalf("metrics.durations = %d, want 1", len(metrics.durations))
	}
}

func TestTenantDashboard_TenantIsolation(t *testing.T) {
	t.Parallel()
	h, _, _ := newDashboardHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t2/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var kpis TenantDashboardKPIs
	if err := json.NewDecoder(w.Body).Decode(&kpis); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if kpis.TenantID != "t2" {
		t.Fatalf("tenant_id = %s, want t2 (isolation check)", kpis.TenantID)
	}
}

func TestTenantDashboard_EmptyTenant(t *testing.T) {
	t.Parallel()
	repo := &inMemoryDashboardRepo{}
	h, err := NewTenantDashboardHandler(nil, TenantDashboardHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewTenantDashboardHandler: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenants/t-empty/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (empty tenant still renders)", w.Code)
	}
	var kpis TenantDashboardKPIs
	if err := json.NewDecoder(w.Body).Decode(&kpis); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if kpis.ActiveOrders != 0 {
		t.Fatalf("active_orders = %d, want 0", kpis.ActiveOrders)
	}
}

func TestTenantDashboard_MissingTenant(t *testing.T) {
	t.Parallel()
	h, _, _ := newDashboardHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing tenant)", w.Code)
	}
}
