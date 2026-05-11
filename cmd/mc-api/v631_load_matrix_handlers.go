package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apihandler "github.com/nfsarch33/agentic-ecommerce/internal/api/handler"
)

type loadMatrixHandlers struct {
	payments        http.Handler
	adminMobile     http.Handler
	tenantDashboard http.Handler
	gmv             http.Handler
}

func buildLoadMatrixHandlers(logger *slog.Logger) (loadMatrixHandlers, error) {
	now := func() time.Time { return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) }

	payments, err := apihandler.NewPaymentsHandler(logger, apihandler.PaymentsHandlerConfig{
		Repository: staticPaymentsRepo{now: now},
	})
	if err != nil {
		return loadMatrixHandlers{}, fmt.Errorf("payments: %w", err)
	}

	admin, err := apihandler.NewAdminMobileHandler(logger, apihandler.AdminMobileHandlerConfig{
		Repository: staticAdminRepo{now: now},
		Now:        now,
	})
	if err != nil {
		return loadMatrixHandlers{}, fmt.Errorf("admin mobile: %w", err)
	}

	dashboard, err := apihandler.NewTenantDashboardHandler(logger, apihandler.TenantDashboardHandlerConfig{
		Repository: staticDashboardRepo{now: now},
		Now:        now,
	})
	if err != nil {
		return loadMatrixHandlers{}, fmt.Errorf("tenant dashboard: %w", err)
	}

	gmv, err := apihandler.NewGMVHandler(logger, apihandler.GMVHandlerConfig{
		Repository: staticGMVRepo{},
		Now:        now,
	})
	if err != nil {
		return loadMatrixHandlers{}, fmt.Errorf("gmv: %w", err)
	}

	return loadMatrixHandlers{
		payments:        payments,
		adminMobile:     admin,
		tenantDashboard: dashboard,
		gmv:             gmv,
	}, nil
}

func (s *server) tenantAdminMux(w http.ResponseWriter, r *http.Request) {
	if s.tenantDashboard != nil && strings.HasSuffix(strings.TrimSuffix(r.URL.Path, "/"), "/dashboard") {
		s.tenantDashboard.ServeHTTP(w, r)
		return
	}
	s.tenantAdminHandler(w, r)
}

type staticPaymentsRepo struct {
	now func() time.Time
}

func (r staticPaymentsRepo) List(_ context.Context, filter apihandler.PaymentsFilter) ([]apihandler.PaymentRow, int, error) {
	rows := []apihandler.PaymentRow{
		{PaymentID: "pay-1", TenantID: filter.TenantID, OrderID: "ord-1", Provider: "stripe", Status: "succeeded", AmountCents: 12900, Currency: "AUD", CreatedAt: r.now()},
		{PaymentID: "pay-2", TenantID: filter.TenantID, OrderID: "ord-2", Provider: "paypal", Status: "pending", AmountCents: 8900, Currency: "AUD", CreatedAt: r.now().Add(-time.Hour)},
		{PaymentID: "pay-3", TenantID: filter.TenantID, OrderID: "ord-3", Provider: "alipay", Status: "succeeded", AmountCents: 15900, Currency: "AUD", CreatedAt: r.now().Add(-2 * time.Hour)},
	}
	filtered := rows[:0]
	for _, row := range rows {
		if filter.Provider != "" && row.Provider != filter.Provider {
			continue
		}
		if filter.Status != "" && row.Status != filter.Status {
			continue
		}
		filtered = append(filtered, row)
	}
	total := len(filtered)
	if filter.Offset >= total {
		return nil, total, nil
	}
	filtered = filtered[filter.Offset:]
	if filter.Limit > 0 && len(filtered) > filter.Limit {
		filtered = filtered[:filter.Limit]
	}
	return filtered, total, nil
}

type staticAdminRepo struct {
	now func() time.Time
}

func (r staticAdminRepo) AdminSummary(context.Context, string) (apihandler.AdminSummary, error) {
	return apihandler.AdminSummary{
		ActiveOrders:  42,
		GMVTodayCents: 512300,
		PendingAlerts: 3,
		ChannelHealth: []apihandler.AdminChannelStatus{
			{Channel: "tiktok", Status: "healthy"},
			{Channel: "facebook", Status: "healthy"},
			{Channel: "rednote", Status: "degraded"},
		},
	}, nil
}

func (r staticAdminRepo) AdminOrders(context.Context, string, int, int) ([]apihandler.AdminOrderRow, int, error) {
	return []apihandler.AdminOrderRow{
		{OrderID: "ord-1", Status: "processing", TotalCents: 12900, Channel: "tiktok", CreatedAt: r.now()},
		{OrderID: "ord-2", Status: "shipped", TotalCents: 8900, Channel: "facebook", CreatedAt: r.now().Add(-time.Hour)},
	}, 2, nil
}

func (r staticAdminRepo) AdminResolveAlert(context.Context, string, string, string, time.Time) error {
	return nil
}

func (r staticAdminRepo) AdminChannels(context.Context, string) ([]apihandler.AdminChannelStatus, error) {
	return []apihandler.AdminChannelStatus{
		{Channel: "tiktok", Status: "healthy"},
		{Channel: "facebook", Status: "healthy"},
		{Channel: "rednote", Status: "degraded"},
	}, nil
}

type staticDashboardRepo struct {
	now func() time.Time
}

func (r staticDashboardRepo) ActiveOrders(context.Context, string) (int64, error) { return 42, nil }
func (r staticDashboardRepo) GMVToday(context.Context, string) (int64, error)     { return 512300, nil }
func (r staticDashboardRepo) GMVMTD(context.Context, string) (int64, error)       { return 9123400, nil }
func (r staticDashboardRepo) PendingAlerts(context.Context, string) (int, error)  { return 3, nil }
func (r staticDashboardRepo) ActiveChannels(context.Context, string) (int, error) { return 4, nil }

func (r staticDashboardRepo) ChannelHealth(context.Context, string) ([]apihandler.ChannelHealthEntry, error) {
	return []apihandler.ChannelHealthEntry{
		{Channel: "tiktok", Status: "healthy"},
		{Channel: "facebook", Status: "healthy"},
		{Channel: "rednote", Status: "degraded"},
	}, nil
}

func (r staticDashboardRepo) RecentAgentActions(context.Context, string, int) ([]apihandler.RecentAgentAction, error) {
	return []apihandler.RecentAgentAction{
		{AgentID: "pricing", Action: "price.change.applied", Status: "applied", Timestamp: r.now()},
	}, nil
}

type staticGMVRepo struct{}

func (staticGMVRepo) Daily(_ context.Context, filter apihandler.GMVFilter) ([]apihandler.GMVDailyPoint, error) {
	return []apihandler.GMVDailyPoint{
		{Day: filter.From, GMVAUDCents: 125000, OrderCount: 12},
		{Day: filter.From.AddDate(0, 0, 1), GMVAUDCents: 141000, OrderCount: 15},
	}, nil
}

func (staticGMVRepo) ByChannel(context.Context, apihandler.GMVFilter) ([]apihandler.GMVChannelPoint, error) {
	return []apihandler.GMVChannelPoint{
		{Channel: "tiktok", GMVAUDCents: 150000, OrderCount: 16},
		{Channel: "facebook", GMVAUDCents: 116000, OrderCount: 11},
	}, nil
}

func (staticGMVRepo) ByProduct(context.Context, apihandler.GMVFilter) ([]apihandler.GMVProductPoint, error) {
	return []apihandler.GMVProductPoint{
		{ProductID: "sku-1", GMVAUDCents: 90000, OrderCount: 8},
		{ProductID: "sku-2", GMVAUDCents: 76000, OrderCount: 7},
	}, nil
}
