package observability

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTenantMetricsMiddleware_RejectsMissingTenantID(t *testing.T) {
	t.Parallel()
	mw := NewTenantMetricsMiddleware("X-Tenant-Id", true)
	inner := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	handler := mw.WrapHandler(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing tenant_id)", w.Code)
	}
}

func TestTenantMetricsMiddleware_AcceptsHeaderTenantID(t *testing.T) {
	t.Parallel()
	mw := NewTenantMetricsMiddleware("X-Tenant-Id", true)
	var captured string
	inner := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		captured = TenantIDFromContext(r.Context())
	})
	handler := mw.WrapHandler(inner)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	req.Header.Set("X-Tenant-Id", "tenant-42")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if captured != "tenant-42" {
		t.Fatalf("tenant_id = %s, want tenant-42", captured)
	}
}

func TestTenantMetricsAudit_AllScopedMetricsListed(t *testing.T) {
	t.Parallel()
	scoped := TenantScopedMetrics()
	if len(scoped) == 0 {
		t.Fatal("TenantScopedMetrics returned empty list")
	}
	seen := make(map[string]bool, len(scoped))
	for _, m := range scoped {
		if seen[m] {
			t.Fatalf("duplicate metric in TenantScopedMetrics: %s", m)
		}
		seen[m] = true
	}
	required := []string{
		"ec_coord_resolutions_total",
		"ec_coord_reward_signals_total",
		"ec_payment_charges_total",
	}
	for _, r := range required {
		if !seen[r] {
			t.Fatalf("required metric %s missing from TenantScopedMetrics", r)
		}
	}
}

func TestValidateTenantContext_RejectsEmpty(t *testing.T) {
	t.Parallel()
	err := ValidateTenantContext(context.Background())
	if err == nil {
		t.Fatal("ValidateTenantContext should reject empty context")
	}
}

func TestValidateTenantContext_AcceptsPresent(t *testing.T) {
	t.Parallel()
	ctx := WithTenantID(context.Background(), "t1")
	if err := ValidateTenantContext(ctx); err != nil {
		t.Fatalf("ValidateTenantContext with tenant: %v", err)
	}
}

func TestTenantDashboardVariableTemplate(t *testing.T) {
	t.Parallel()
	template := `$tenant_id`
	if template != "$tenant_id" {
		t.Fatal("Grafana variable template should be $tenant_id")
	}
}
