package main

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

func TestTenantSettingsEndpointRequiresTenantAndStoresPerTenant(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	missing := httptest.NewRequest(http.MethodGet, "/api/v1/tenant/settings", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, missing)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing tenant status = %d, want 400; body=%s", rec.Code, rec.Body.String())
	}

	bodyA := `{"branding":{"store_name":"Tenant A","primary_color":"#111111"},"woocommerce":{"store_url":"https://a.example","consumer_key_ref":"secret/a/key","consumer_secret_ref":"secret/a/secret"},"ai":{"content_tone":"friendly","model_tier":"fast","auto_generate_seo":true,"fact_check_required":true},"compliance":{"disabled_rule_ids":["seo_minimum_score"],"severity_override":{"image_alt_text":"warning"},"seo_score_min":82}}`
	putA := httptest.NewRequest(http.MethodPut, "/api/v1/tenant/settings", strings.NewReader(bodyA))
	putA.Header.Set("Content-Type", "application/json")
	putA.Header.Set("X-Tenant-ID", "tenant-a")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, putA)
	if rec.Code != http.StatusOK {
		t.Fatalf("put tenant A status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	bodyB := `{"branding":{"store_name":"Tenant B","primary_color":"#222222"},"woocommerce":{"store_url":"https://b.example"},"ai":{"content_tone":"technical","model_tier":"quality"},"compliance":{"seo_score_min":70}}`
	putB := httptest.NewRequest(http.MethodPut, "/api/v1/tenant/settings", strings.NewReader(bodyB))
	putB.Header.Set("Content-Type", "application/json")
	putB.Header.Set("X-Tenant-ID", "tenant-b")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, putB)
	if rec.Code != http.StatusOK {
		t.Fatalf("put tenant B status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	getA := httptest.NewRequest(http.MethodGet, "/api/v1/tenant/settings", nil)
	getA.Header.Set("X-Tenant-ID", "tenant-a")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, getA)
	if rec.Code != http.StatusOK {
		t.Fatalf("get tenant A status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got tenant.Settings
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if got.TenantID != "tenant-a" || got.Branding.StoreName != "Tenant A" || got.Compliance.SEOScoreMin != 82 {
		t.Fatalf("tenant A settings = %+v", got)
	}
}

func TestTenantMiddlewareExtractsTenantFromJWTClaims(t *testing.T) {
	srv := secureTestServer(t, nil)
	defer srv.Close()
	token := mintTestAccessTokenForTenant(t, srv, "operator@example.com", security.RoleOperator, "tenant-jwt")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/tenant/settings", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got tenant.Settings
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode settings: %v", err)
	}
	if got.TenantID != "tenant-jwt" {
		t.Fatalf("tenant_id = %q, want tenant-jwt", got.TenantID)
	}
}

func TestCustomRulesEndpointCreatesAndListsPerTenant(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	created := createCustomRuleAPI(t, srv, "tenant-a")
	if created.TenantID != "tenant-a" || created.Version != 1 {
		t.Fatalf("created rule = %+v", created)
	}

	tenantB := listCustomRulesAPI(t, srv, "tenant-b")
	if len(tenantB.Rules) != 0 {
		t.Fatalf("tenant B rules = %+v, want isolated empty list", tenantB.Rules)
	}
}

func TestCustomRulesEndpointUpdatesVersionAndEnabledState(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	createCustomRuleAPI(t, srv, "tenant-a")

	updateBody := `{"name":"No greenwashing v2","description":"Reject unsupported claims","severity":"warning","enabled":false,"definition":{"type":"contains_any","field":"description","values":["carbon neutral"],"fail_reason":"unsupported sustainability claim"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/compliance/custom-rules/no-greenwashing", strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var updated compliance.CustomRule
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode updated rule: %v", err)
	}
	if updated.Version != 2 || updated.Enabled {
		t.Fatalf("updated rule = %+v, want version 2 disabled", updated)
	}
}

func TestCustomRulesEndpointValidatesDefinitions(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/custom-rules", strings.NewReader(`{"id":"bad","name":"Bad","severity":"critical","enabled":true,"definition":{"type":"contains_any","field":"description"}}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestComplianceReportsSummaryIsTenantScoped(t *testing.T) {
	t.Parallel()

	srv, _ := seedComplianceReport(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports/summary", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("summary status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var summary compliance.Summary
	if err := json.NewDecoder(rec.Body).Decode(&summary); err != nil {
		t.Fatalf("decode summary: %v", err)
	}
	if summary.TenantID != "tenant-a" || summary.TotalChecks != 1 || summary.FailedChecks != 1 {
		t.Fatalf("summary = %+v", summary)
	}
	if summary.RuleStats["prohibited_words"].Failed != 1 {
		t.Fatalf("rule stats = %+v", summary.RuleStats)
	}
}

func TestComplianceReportsExportCSVIsTenantScoped(t *testing.T) {
	t.Parallel()

	srv, productID := seedComplianceReport(t)

	export := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports/export?format=csv", nil)
	export.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, export)
	if rec.Code != http.StatusOK {
		t.Fatalf("export status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "text/csv" {
		t.Fatalf("content-type = %q, want text/csv", got)
	}
	rows, err := csv.NewReader(strings.NewReader(rec.Body.String())).ReadAll()
	if err != nil {
		t.Fatalf("read csv: %v\n%s", err, rec.Body.String())
	}
	if len(rows) != 2 || rows[1][0] != "tenant-a" || rows[1][1] != productID {
		t.Fatalf("csv rows = %+v", rows)
	}
}

func createCustomRuleAPI(t *testing.T, srv *server, tenantID string) compliance.CustomRule {
	t.Helper()
	createBody := `{"id":"no-greenwashing","name":"No greenwashing","description":"Reject unsupported claims","severity":"error","enabled":true,"definition":{"type":"contains_any","field":"description","values":["carbon neutral"],"fail_reason":"unsupported sustainability claim"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/custom-rules", strings.NewReader(createBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created compliance.CustomRule
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created rule: %v", err)
	}
	return created
}

func listCustomRulesAPI(t *testing.T, srv *server, tenantID string) customRulesResponse {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/custom-rules", nil)
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp customRulesResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode rules: %v", err)
	}
	return resp
}

func seedComplianceReport(t *testing.T) (*server, string) {
	t.Helper()
	srv, repo := testServer(t)
	product := addProductWithContent(t, repo, catalog.ProductInput{
		SKU:         "BAD-CLAIM",
		Title:       "Miracle Band",
		Description: "Cheap miracle cure band.",
		Images:      []catalog.Image{{URL: "https://cdn.example.com/bad.jpg"}},
	})
	body := `{"keywords":["miracle cure"],"seo_score_min":90}`
	check := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/compliance-check", bytes.NewBufferString(body))
	check.Header.Set("Content-Type", "application/json")
	check.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, check)
	if rec.Code != http.StatusOK {
		t.Fatalf("check status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	return srv, product.ID().String()
}

func mintTestAccessTokenForTenant(t *testing.T, srv *server, subject string, role security.Role, tenantID string) string {
	t.Helper()
	token, err := srv.tokenManager.MintAccessToken(security.Principal{Subject: subject, Role: role, TenantID: tenantID})
	if err != nil {
		t.Fatalf("mint test token: %v", err)
	}
	return token
}
