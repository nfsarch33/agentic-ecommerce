package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	apihandler "github.com/nfsarch33/helixon-ec/internal/api/handler"
)

type loadMatrixHandlers struct {
	payments        http.Handler
	adminMobile     http.Handler
	operatorAlerts  http.Handler
	tenantDashboard http.Handler
	gmv             http.Handler
}

type loadMatrixHandlerBuilder struct {
	name   string
	build  func() (http.Handler, error)
	assign func(*loadMatrixHandlers, http.Handler)
}

func buildLoadMatrixHandlers(logger *slog.Logger) (loadMatrixHandlers, error) {
	now := func() time.Time { return time.Date(2026, 5, 11, 0, 0, 0, 0, time.UTC) }

	builds := []loadMatrixHandlerBuilder{
		{
			name:  "payments",
			build: func() (http.Handler, error) { return buildStaticPaymentsHandler(logger, now) },
			assign: func(handlers *loadMatrixHandlers, handler http.Handler) {
				handlers.payments = handler
			},
		},
		{
			name:  "admin mobile",
			build: func() (http.Handler, error) { return buildStaticAdminMobileHandler(logger, now) },
			assign: func(handlers *loadMatrixHandlers, handler http.Handler) {
				handlers.adminMobile = handler
			},
		},
		{
			name:  "operator alerts",
			build: func() (http.Handler, error) { return buildStaticOperatorAlertsHandler(logger, now) },
			assign: func(handlers *loadMatrixHandlers, handler http.Handler) {
				handlers.operatorAlerts = handler
			},
		},
		{
			name:  "tenant dashboard",
			build: func() (http.Handler, error) { return buildStaticTenantDashboardHandler(logger, now) },
			assign: func(handlers *loadMatrixHandlers, handler http.Handler) {
				handlers.tenantDashboard = handler
			},
		},
		{
			name:  "gmv",
			build: func() (http.Handler, error) { return buildStaticGMVHandler(logger, now) },
			assign: func(handlers *loadMatrixHandlers, handler http.Handler) {
				handlers.gmv = handler
			},
		},
	}

	var handlers loadMatrixHandlers
	for _, build := range builds {
		handler, err := build.build()
		if err != nil {
			return loadMatrixHandlers{}, fmt.Errorf("%s: %w", build.name, err)
		}
		build.assign(&handlers, handler)
	}

	return handlers, nil
}

func buildStaticPaymentsHandler(logger *slog.Logger, now func() time.Time) (http.Handler, error) {
	return apihandler.NewPaymentsHandler(logger, apihandler.PaymentsHandlerConfig{
		Repository: staticPaymentsRepo{now: now},
	})
}

func buildStaticAdminMobileHandler(logger *slog.Logger, now func() time.Time) (http.Handler, error) {
	return apihandler.NewAdminMobileHandler(logger, apihandler.AdminMobileHandlerConfig{
		Repository: staticAdminRepo{now: now},
		Now:        now,
	})
}

func buildStaticTenantDashboardHandler(logger *slog.Logger, now func() time.Time) (http.Handler, error) {
	return apihandler.NewTenantDashboardHandler(logger, apihandler.TenantDashboardHandlerConfig{
		Repository: staticDashboardRepo{now: now},
		Now:        now,
	})
}

func buildStaticGMVHandler(logger *slog.Logger, now func() time.Time) (http.Handler, error) {
	return apihandler.NewGMVHandler(logger, apihandler.GMVHandlerConfig{
		Repository: staticGMVRepo{},
		Now:        now,
	})
}

func buildStaticOperatorAlertsHandler(logger *slog.Logger, now func() time.Time) (http.Handler, error) {
	repo := apihandler.NewInMemoryOperatorAlertRepository()
	if err := repo.Insert(context.Background(), apihandler.OperatorAlert{
		TenantID:  "load-test-tenant",
		AlertID:   "alert-1",
		AlertType: apihandler.AlertTypeChannelStatusFail,
		Severity:  apihandler.AlertSeverityWarning,
		CreatedAt: now(),
	}); err != nil {
		return nil, fmt.Errorf("seed static alert: %w", err)
	}

	handler, err := apihandler.NewOperatorAlertHandler(logger, apihandler.OperatorAlertHandlerConfig{
		Repository: repo,
		Now:        now,
	})
	if err != nil {
		return nil, err
	}
	return handler, nil
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
