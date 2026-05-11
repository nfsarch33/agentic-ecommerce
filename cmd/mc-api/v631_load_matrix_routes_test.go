package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
)

func TestV631LoadMatrixRoutesAreMounted(t *testing.T) {
	t.Parallel()

	srv := newServer(slog.New(slog.NewJSONHandler(io.Discard, nil)), newSeededProductRepository(), inmemory.NewOrderRepository(), inmemory.NewCartRepository())
	srv.rateLimiter = nil
	t.Cleanup(srv.Close)

	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "payments", method: http.MethodGet, path: "/api/v1/payments?tenant_id=load-test-tenant&limit=20"},
		{name: "webhooks", method: http.MethodGet, path: "/api/v1/webhooks"},
		{name: "admin_summary", method: http.MethodGet, path: "/api/v1/admin/summary"},
		{name: "admin_orders", method: http.MethodGet, path: "/api/v1/admin/orders?page=1&limit=20"},
		{name: "marketplace_plugins", method: http.MethodGet, path: "/api/v1/marketplace/plugins?per_page=20"},
		{name: "tenant_dashboard", method: http.MethodGet, path: "/api/v1/tenants/load-test-tenant/dashboard"},
		{name: "gmv_daily", method: http.MethodGet, path: "/api/v1/analytics/gmv?tenant_id=load-test-tenant&from=2026-05-01&to=2026-05-31"},
	}

	handler := srv.mux()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(tt.method, tt.path, nil)
			req.Header.Set("X-Tenant-ID", "load-test-tenant")
			rec := httptest.NewRecorder()

			handler.ServeHTTP(rec, req)

			if rec.Code < http.StatusOK || rec.Code >= http.StatusMultipleChoices {
				t.Fatalf("%s %s returned %d; load matrix routes must return 2xx, body=%s", tt.method, tt.path, rec.Code, rec.Body.String())
			}
		})
	}
}
