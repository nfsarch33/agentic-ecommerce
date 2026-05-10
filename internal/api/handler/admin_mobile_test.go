package handler

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

type inMemoryAdminRepo struct {
	summary  AdminSummary
	orders   []AdminOrderRow
	total    int
	channels []AdminChannelStatus

	resolvedAlerts []resolvedAlertRecord
}

type resolvedAlertRecord struct {
	TenantID string
	AlertID  string
	Action   string
}

func (r *inMemoryAdminRepo) AdminSummary(_ context.Context, _ string) (AdminSummary, error) {
	return r.summary, nil
}

func (r *inMemoryAdminRepo) AdminOrders(_ context.Context, _ string, _, _ int) ([]AdminOrderRow, int, error) {
	return r.orders, r.total, nil
}

func (r *inMemoryAdminRepo) AdminResolveAlert(_ context.Context, tenantID, alertID, action string, _ time.Time) error {
	r.resolvedAlerts = append(r.resolvedAlerts, resolvedAlertRecord{
		TenantID: tenantID,
		AlertID:  alertID,
		Action:   action,
	})
	return nil
}

func (r *inMemoryAdminRepo) AdminChannels(_ context.Context, _ string) ([]AdminChannelStatus, error) {
	return r.channels, nil
}

type recordingAdminMetrics struct {
	durations map[string][]float64
}

func (m *recordingAdminMetrics) ObserveAdminAPIDuration(endpoint string, d float64) {
	if m.durations == nil {
		m.durations = make(map[string][]float64)
	}
	m.durations[endpoint] = append(m.durations[endpoint], d)
}

func newAdminHarness(t *testing.T) (*AdminMobileHandler, *inMemoryAdminRepo, *recordingAdminMetrics) {
	t.Helper()
	repo := &inMemoryAdminRepo{
		summary: AdminSummary{
			ActiveOrders:  15,
			GMVTodayCents: 750000,
			PendingAlerts: 2,
			ChannelHealth: []AdminChannelStatus{
				{Channel: "tiktok", Status: "healthy"},
				{Channel: "instagram", Status: "degraded"},
			},
		},
		orders: []AdminOrderRow{
			{OrderID: "ord-1", Status: "processing", TotalCents: 5000, Channel: "tiktok", CreatedAt: time.Date(2026, 5, 10, 10, 0, 0, 0, time.UTC)},
			{OrderID: "ord-2", Status: "shipped", TotalCents: 12000, Channel: "instagram", CreatedAt: time.Date(2026, 5, 10, 11, 0, 0, 0, time.UTC)},
		},
		total: 45,
		channels: []AdminChannelStatus{
			{Channel: "tiktok", Status: "healthy"},
			{Channel: "instagram", Status: "degraded"},
			{Channel: "facebook", Status: "healthy"},
		},
	}
	metrics := &recordingAdminMetrics{}
	h, err := NewAdminMobileHandler(nil, AdminMobileHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
		Metrics:    metrics,
	})
	if err != nil {
		t.Fatalf("NewAdminMobileHandler: %v", err)
	}
	return h, repo, metrics
}

func TestAdminMobile_Summary(t *testing.T) {
	t.Parallel()
	h, _, metrics := newAdminHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp map[string]json.RawMessage
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var summary AdminSummary
	if err := json.Unmarshal(resp["data"], &summary); err != nil {
		t.Fatalf("unmarshal data: %v", err)
	}
	if summary.ActiveOrders != 15 {
		t.Fatalf("active_orders = %d, want 15", summary.ActiveOrders)
	}
	if summary.GMVTodayCents != 750000 {
		t.Fatalf("gmv_today = %d, want 750000", summary.GMVTodayCents)
	}
	if summary.PendingAlerts != 2 {
		t.Fatalf("pending_alerts = %d, want 2", summary.PendingAlerts)
	}
	if len(metrics.durations["summary"]) != 1 {
		t.Fatalf("summary metrics = %d, want 1", len(metrics.durations["summary"]))
	}
}

func TestAdminMobile_PaginatedOrders(t *testing.T) {
	t.Parallel()
	h, _, _ := newAdminHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders?page=1&limit=20", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Data  []AdminOrderRow `json:"data"`
		Meta  PaginationMeta  `json:"meta"`
		Links PaginationLinks `json:"links"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 2 {
		t.Fatalf("orders = %d, want 2", len(resp.Data))
	}
	if resp.Meta.Page != 1 {
		t.Fatalf("page = %d, want 1", resp.Meta.Page)
	}
	if resp.Meta.Total != 45 {
		t.Fatalf("total = %d, want 45", resp.Meta.Total)
	}
	if resp.Links.Next == "" {
		t.Fatal("expected next link for page 1 of 45 items")
	}
	if resp.Links.Prev != "" {
		t.Fatal("unexpected prev link on page 1")
	}
}

func TestAdminMobile_AlertAction(t *testing.T) {
	t.Parallel()
	h, repo, _ := newAdminHarness(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/alerts/alert-42/action?action=approve", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if len(repo.resolvedAlerts) != 1 {
		t.Fatalf("resolved = %d, want 1", len(repo.resolvedAlerts))
	}
	if repo.resolvedAlerts[0].AlertID != "alert-42" {
		t.Fatalf("alert_id = %s, want alert-42", repo.resolvedAlerts[0].AlertID)
	}
	if repo.resolvedAlerts[0].Action != "approve" {
		t.Fatalf("action = %s, want approve", repo.resolvedAlerts[0].Action)
	}
}

func TestAdminMobile_ChannelHealth(t *testing.T) {
	t.Parallel()
	h, _, _ := newAdminHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/channels", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}

	var resp struct {
		Data []AdminChannelStatus `json:"data"`
	}
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Data) != 3 {
		t.Fatalf("channels = %d, want 3", len(resp.Data))
	}
}

func TestAdminMobile_TenantIsolation(t *testing.T) {
	t.Parallel()
	h, _, _ := newAdminHarness(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing tenant)", w.Code)
	}
}
