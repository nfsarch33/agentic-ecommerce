package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
)

func newOperatorAlertsContractServer(t *testing.T) *server {
	t.Helper()

	srv := newServer(
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		inmemory.NewProductRepository(),
		inmemory.NewOrderRepository(),
		inmemory.NewCartRepository(),
	)
	srv.rateLimiter = nil
	t.Cleanup(srv.Close)
	return srv
}

func assertOperatorAlertRouteMounted(t *testing.T, method, path string) {
	t.Helper()

	srv := newOperatorAlertsContractServer(t)

	req := httptest.NewRequest(method, path, nil)
	req.Header.Set("X-Tenant-Id", "load-test-tenant")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code < http.StatusOK || rec.Code >= http.StatusMultipleChoices {
		t.Fatalf("%s %s returned %d; operator-alert API must be mounted and functional, body=%s", method, path, rec.Code, rec.Body.String())
	}
}

func TestOperatorAlertListRouteIsMounted(t *testing.T) {
	t.Parallel()

	assertOperatorAlertRouteMounted(t, http.MethodGet, "/api/v1/operator/alerts?tenant_id=load-test-tenant&status=pending")
}

func TestOperatorAlertAcknowledgeRouteIsMounted(t *testing.T) {
	t.Parallel()

	assertOperatorAlertRouteMounted(t, http.MethodPost, "/api/v1/operator/alerts/alert-1/acknowledge?tenant_id=load-test-tenant")
}

func TestOperatorAlertResolveRouteIsMounted(t *testing.T) {
	t.Parallel()

	assertOperatorAlertRouteMounted(t, http.MethodPost, "/api/v1/operator/alerts/alert-1/resolve?tenant_id=load-test-tenant&action=approve")
}

func TestOperatorAlertsOpenAPIContracts(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")

	assertOperation(t, paths, "/api/v1/operator/alerts", "get", "listOperatorAlerts", []string{"200", "400", "401", "403", "500"})
	assertOperation(t, paths, "/api/v1/operator/alerts/{alert_id}/acknowledge", "post", "acknowledgeOperatorAlert", []string{"200", "400", "401", "403", "404", "409", "500"})
	assertOperation(t, paths, "/api/v1/operator/alerts/{alert_id}/resolve", "post", "resolveOperatorAlert", []string{"200", "400", "401", "403", "404", "409", "500"})

	schemas := specMap(t, specMap(t, spec, "components"), "schemas")
	assertRequiredFields(t, schemas, "OperatorAlert", []string{
		"tenant_id",
		"alert_id",
		"alert_type",
		"severity",
		"status",
		"created_at",
		"expires_at",
	})
	assertRequiredFields(t, schemas, "OperatorAlertListResponse", []string{"tenant_id", "status", "alerts", "count"})
	assertRequiredFields(t, schemas, "OperatorAlertAcknowledgeResponse", []string{"tenant_id", "alert_id", "status", "acknowledged_at"})
	assertRequiredFields(t, schemas, "OperatorAlertResolveResponse", []string{"tenant_id", "alert_id", "status", "action_taken", "resolved_at"})
}

func TestOperatorAlertRepresentativeListContract(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPIContract(t)
	srv := newOperatorAlertsContractServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/operator/alerts?tenant_id=load-test-tenant&status=pending", nil)
	req.Header.Set("X-Tenant-Id", "load-test-tenant")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", rec.Code, http.StatusOK, rec.Body.String())
	}

	var payload map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &payload)
	assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/operator/alerts", http.MethodGet, "200"), payload)
	assertOrUpdateContractGolden(
		t,
		filepath.Join("testdata", "contracts", "operator_alerts_list.golden.json"),
		normalizeContractPayload(payload),
	)
}
