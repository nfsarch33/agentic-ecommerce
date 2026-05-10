//go:build v481_smoke

package v481

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/api/handler"
)

type stubAdminRepo struct {
	summaryByTenant  map[string]handler.AdminSummary
	ordersByTenant   map[string][]handler.AdminOrderRow
	channelsByTenant map[string][]handler.AdminChannelStatus
	resolvedAlerts   []alertRecord
}

type alertRecord struct {
	TenantID, AlertID, Action string
}

func (r *stubAdminRepo) AdminSummary(_ context.Context, tenantID string) (handler.AdminSummary, error) {
	return r.summaryByTenant[tenantID], nil
}

func (r *stubAdminRepo) AdminOrders(_ context.Context, tenantID string, page, limit int) ([]handler.AdminOrderRow, int, error) {
	all := r.ordersByTenant[tenantID]
	total := len(all)
	start := (page - 1) * limit
	if start >= total {
		return nil, total, nil
	}
	end := start + limit
	if end > total {
		end = total
	}
	return all[start:end], total, nil
}

func (r *stubAdminRepo) AdminResolveAlert(_ context.Context, tenantID, alertID, action string, _ time.Time) error {
	r.resolvedAlerts = append(r.resolvedAlerts, alertRecord{tenantID, alertID, action})
	return nil
}

func (r *stubAdminRepo) AdminChannels(_ context.Context, tenantID string) ([]handler.AdminChannelStatus, error) {
	return r.channelsByTenant[tenantID], nil
}

func newE2EAdminHandler(t *testing.T) (*handler.AdminMobileHandler, *stubAdminRepo) {
	t.Helper()
	repo := &stubAdminRepo{
		summaryByTenant: map[string]handler.AdminSummary{
			"t1": {ActiveOrders: 10, GMVTodayCents: 500000, PendingAlerts: 3, ChannelHealth: []handler.AdminChannelStatus{{Channel: "tiktok", Status: "healthy"}}},
			"t2": {ActiveOrders: 5, GMVTodayCents: 200000, PendingAlerts: 1},
		},
		ordersByTenant: map[string][]handler.AdminOrderRow{
			"t1": {
				{OrderID: "o1", Status: "processing", TotalCents: 5000, Channel: "tiktok"},
				{OrderID: "o2", Status: "shipped", TotalCents: 8000, Channel: "instagram"},
				{OrderID: "o3", Status: "delivered", TotalCents: 3000, Channel: "facebook"},
			},
		},
		channelsByTenant: map[string][]handler.AdminChannelStatus{
			"t1": {{Channel: "tiktok", Status: "healthy"}, {Channel: "instagram", Status: "degraded"}},
		},
	}
	h, err := handler.NewAdminMobileHandler(nil, handler.AdminMobileHandlerConfig{
		Repository: repo,
		Now:        func() time.Time { return time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewAdminMobileHandler: %v", err)
	}
	return h, repo
}

// Scenario 1: Full mobile API flow summary → orders → alert → channels
func TestAdminAPI_FullMobileFlow(t *testing.T) {
	t.Parallel()
	h, repo := newE2EAdminHandler(t)

	// Step 1: Summary
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("summary status = %d, want 200", w.Code)
	}

	// Step 2: Orders
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders?page=1&limit=2", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("orders status = %d, want 200", w.Code)
	}

	// Step 3: Alert action
	req = httptest.NewRequest(http.MethodPost, "/api/v1/admin/alerts/a1/action?action=approve", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("alert action status = %d, want 200", w.Code)
	}
	if len(repo.resolvedAlerts) != 1 {
		t.Fatalf("resolved alerts = %d, want 1", len(repo.resolvedAlerts))
	}

	// Step 4: Channels
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/channels", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("channels status = %d, want 200", w.Code)
	}
}

// Scenario 2: Pagination correctness
func TestAdminAPI_PaginationCorrectness(t *testing.T) {
	t.Parallel()
	h, _ := newE2EAdminHandler(t)

	// Page 1
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders?page=1&limit=2", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var page1 struct {
		Data  []handler.AdminOrderRow `json:"data"`
		Meta  handler.PaginationMeta  `json:"meta"`
		Links handler.PaginationLinks `json:"links"`
	}
	_ = json.NewDecoder(w.Body).Decode(&page1)
	if len(page1.Data) != 2 {
		t.Fatalf("page1 orders = %d, want 2", len(page1.Data))
	}
	if page1.Meta.Total != 3 {
		t.Fatalf("total = %d, want 3", page1.Meta.Total)
	}
	if page1.Links.Next == "" {
		t.Fatal("expected next link on page 1")
	}

	// Page 2
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/orders?page=2&limit=2", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var page2 struct {
		Data  []handler.AdminOrderRow `json:"data"`
		Meta  handler.PaginationMeta  `json:"meta"`
		Links handler.PaginationLinks `json:"links"`
	}
	_ = json.NewDecoder(w.Body).Decode(&page2)
	if len(page2.Data) != 1 {
		t.Fatalf("page2 orders = %d, want 1", len(page2.Data))
	}
	if page2.Links.Prev == "" {
		t.Fatal("expected prev link on page 2")
	}
}

// Scenario 3: Tenant isolation across all admin endpoints
func TestAdminAPI_TenantIsolation(t *testing.T) {
	t.Parallel()
	h, _ := newE2EAdminHandler(t)

	// t1 summary
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
	req.Header.Set("X-Tenant-Id", "t1")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var t1Resp struct {
		Data handler.AdminSummary `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&t1Resp)
	if t1Resp.Data.ActiveOrders != 10 {
		t.Fatalf("t1 active_orders = %d, want 10", t1Resp.Data.ActiveOrders)
	}

	// t2 summary (different data)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
	req.Header.Set("X-Tenant-Id", "t2")
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)

	var t2Resp struct {
		Data handler.AdminSummary `json:"data"`
	}
	_ = json.NewDecoder(w.Body).Decode(&t2Resp)
	if t2Resp.Data.ActiveOrders != 5 {
		t.Fatalf("t2 active_orders = %d, want 5", t2Resp.Data.ActiveOrders)
	}

	// No tenant header -> 400
	req = httptest.NewRequest(http.MethodGet, "/api/v1/admin/summary", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("no-tenant status = %d, want 400", w.Code)
	}
}
