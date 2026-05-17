package main

import (
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	apihandler "github.com/nfsarch33/agentic-ecommerce/internal/api/handler"
)

func TestBuildLoadMatrixHandlersReturnsAllHandlers(t *testing.T) {
	t.Parallel()

	handlers, err := buildLoadMatrixHandlers(slog.Default())
	if err != nil {
		t.Fatalf("buildLoadMatrixHandlers: %v", err)
	}
	for _, tt := range []struct {
		name    string
		handler http.Handler
	}{
		{name: "payments", handler: handlers.payments},
		{name: "admin_mobile", handler: handlers.adminMobile},
		{name: "operator_alerts", handler: handlers.operatorAlerts},
		{name: "tenant_dashboard", handler: handlers.tenantDashboard},
		{name: "gmv", handler: handlers.gmv},
	} {
		if tt.handler == nil {
			t.Fatalf("expected %s handler to be configured: %#v", tt.name, handlers)
		}
	}
}

func TestTenantAdminMuxRoutesDashboardSuffix(t *testing.T) {
	t.Parallel()

	s := &server{
		tenantDashboard: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusAccepted)
		}),
	}
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/admin/tenants/t-1/dashboard/", nil)

	s.tenantAdminMux(rec, req)

	if rec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want %d", rec.Code, http.StatusAccepted)
	}
}

func TestStaticPaymentsRepoFiltersAndPaginates(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	repo := staticPaymentsRepo{now: func() time.Time { return now }}

	rows, total, err := repo.List(context.Background(), apihandler.PaymentsFilter{
		TenantID: "tenant-1",
		Provider: "stripe",
		Status:   "succeeded",
		Limit:    1,
	})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(rows) != 1 {
		t.Fatalf("rows=%d total=%d, want rows=1 total=1", len(rows), total)
	}
	if rows[0].TenantID != "tenant-1" || rows[0].Provider != "stripe" || rows[0].Status != "succeeded" {
		t.Fatalf("unexpected payment row: %#v", rows[0])
	}

	rows, total, err = repo.List(context.Background(), apihandler.PaymentsFilter{
		TenantID: "tenant-1",
		Offset:   99,
	})
	if err != nil {
		t.Fatalf("List offset: %v", err)
	}
	if total != 3 || rows != nil {
		t.Fatalf("offset rows=%#v total=%d, want nil rows total=3", rows, total)
	}
}

func TestStaticAdminRepoReturnsSummaryOrdersAndChannels(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	repo := staticAdminRepo{now: func() time.Time { return now }}

	summary, err := repo.AdminSummary(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("AdminSummary: %v", err)
	}
	if summary.ActiveOrders != 42 || summary.PendingAlerts != 3 || len(summary.ChannelHealth) != 3 {
		t.Fatalf("unexpected summary: %#v", summary)
	}

	orders, total, err := repo.AdminOrders(context.Background(), "tenant-1", 0, 10)
	if err != nil {
		t.Fatalf("AdminOrders: %v", err)
	}
	if total != 2 || len(orders) != 2 || !orders[0].CreatedAt.Equal(now) {
		t.Fatalf("unexpected orders total=%d rows=%#v", total, orders)
	}

	if err := repo.AdminResolveAlert(context.Background(), "tenant-1", "alert-1", "done", now); err != nil {
		t.Fatalf("AdminResolveAlert: %v", err)
	}

	channels, err := repo.AdminChannels(context.Background(), "tenant-1")
	if err != nil {
		t.Fatalf("AdminChannels: %v", err)
	}
	if len(channels) != 3 || channels[2].Status != "degraded" {
		t.Fatalf("unexpected channels: %#v", channels)
	}
}

func TestStaticDashboardRepoReturnsKPIRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	repo := staticDashboardRepo{now: func() time.Time { return now }}
	ctx := context.Background()

	if got, err := repo.ActiveOrders(ctx, "tenant-1"); err != nil || got != 42 {
		t.Fatalf("ActiveOrders=%d err=%v, want 42 nil", got, err)
	}
	if got, err := repo.GMVToday(ctx, "tenant-1"); err != nil || got != 512300 {
		t.Fatalf("GMVToday=%d err=%v, want 512300 nil", got, err)
	}
	if got, err := repo.GMVMTD(ctx, "tenant-1"); err != nil || got != 9123400 {
		t.Fatalf("GMVMTD=%d err=%v, want 9123400 nil", got, err)
	}
	if got, err := repo.PendingAlerts(ctx, "tenant-1"); err != nil || got != 3 {
		t.Fatalf("PendingAlerts=%d err=%v, want 3 nil", got, err)
	}
	if got, err := repo.ActiveChannels(ctx, "tenant-1"); err != nil || got != 4 {
		t.Fatalf("ActiveChannels=%d err=%v, want 4 nil", got, err)
	}

	health, err := repo.ChannelHealth(ctx, "tenant-1")
	if err != nil {
		t.Fatalf("ChannelHealth: %v", err)
	}
	if len(health) != 3 || health[2].Status != "degraded" {
		t.Fatalf("unexpected health: %#v", health)
	}

	actions, err := repo.RecentAgentActions(ctx, "tenant-1", 5)
	if err != nil {
		t.Fatalf("RecentAgentActions: %v", err)
	}
	if len(actions) != 1 || actions[0].AgentID != "pricing" || !actions[0].Timestamp.Equal(now) {
		t.Fatalf("unexpected actions: %#v", actions)
	}
}

func TestStaticGMVRepoReturnsDailyChannelAndProductSeries(t *testing.T) {
	t.Parallel()

	from := time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC)
	repo := staticGMVRepo{}
	filter := apihandler.GMVFilter{From: from}

	daily, err := repo.Daily(context.Background(), filter)
	if err != nil {
		t.Fatalf("Daily: %v", err)
	}
	if len(daily) != 2 || !daily[0].Day.Equal(from) || daily[1].OrderCount != 15 {
		t.Fatalf("unexpected daily series: %#v", daily)
	}

	channels, err := repo.ByChannel(context.Background(), filter)
	if err != nil {
		t.Fatalf("ByChannel: %v", err)
	}
	if len(channels) != 2 || channels[0].Channel != "tiktok" {
		t.Fatalf("unexpected channel series: %#v", channels)
	}

	products, err := repo.ByProduct(context.Background(), filter)
	if err != nil {
		t.Fatalf("ByProduct: %v", err)
	}
	if len(products) != 2 || products[0].ProductID != "sku-1" {
		t.Fatalf("unexpected product series: %#v", products)
	}
}
