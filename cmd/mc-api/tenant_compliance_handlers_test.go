package main

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/compliance"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/rag"
	"github.com/nfsarch33/helixon-ec/internal/security"
	"github.com/nfsarch33/helixon-ec/internal/tenant"
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

func TestCustomRulesEndpointRejectsInvalidUpdates(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	createCustomRuleAPI(t, srv, "tenant-a")

	updateBody := `{"name":"No greenwashing","description":"Reject unsupported claims","severity":"bad","enabled":true,"definition":{"type":"contains_any","field":"description","values":["carbon neutral"]}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/compliance/custom-rules/no-greenwashing", strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid update status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestCustomRulesEndpointValidatesDefinitions(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	for _, body := range []string{
		`{"id":"bad","name":"Bad","severity":"critical","enabled":true,"definition":{"type":"contains_any","field":"description"}}`,
		`{"id":"bad-type","name":"Bad type","severity":"critical","enabled":true,"definition":{"type":"regex","field":"description","values":["unsafe"]}}`,
		`{"id":"bad-field","name":"Bad field","severity":"critical","enabled":true,"definition":{"type":"contains_any","field":"supplier_cost","values":["secret"]}}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/compliance/custom-rules", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("X-Tenant-ID", "tenant-a")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)

		if rec.Code != http.StatusUnprocessableEntity {
			t.Fatalf("body %s status = %d, want 422; response=%s", body, rec.Code, rec.Body.String())
		}
	}
}

func TestCustomRulesEndpointVersioningIsPerTenant(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	createCustomRuleAPI(t, srv, "tenant-a")
	createCustomRuleAPI(t, srv, "tenant-b")

	updateBody := `{"name":"No greenwashing v2","description":"Reject unsupported claims","severity":"warning","enabled":true,"definition":{"type":"contains_any","field":"description","values":["carbon neutral"],"fail_reason":"unsupported sustainability claim"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/compliance/custom-rules/no-greenwashing", strings.NewReader(updateBody))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("update tenant A status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	tenantA := listCustomRulesAPI(t, srv, "tenant-a")
	tenantB := listCustomRulesAPI(t, srv, "tenant-b")
	if tenantA.Rules[0].Version != 2 {
		t.Fatalf("tenant A version = %d, want 2", tenantA.Rules[0].Version)
	}
	if tenantB.Rules[0].Version != 1 {
		t.Fatalf("tenant B version = %d, want isolated version 1", tenantB.Rules[0].Version)
	}
}

func TestCustomRulesEndpointDeleteIsTenantScoped(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	createCustomRuleAPI(t, srv, "tenant-a")
	createCustomRuleAPI(t, srv, "tenant-b")

	crossDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/compliance/custom-rules/no-greenwashing", nil)
	crossDelete.Header.Set("X-Tenant-ID", "tenant-b")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, crossDelete)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete tenant B own rule status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}

	deleteAgain := httptest.NewRequest(http.MethodDelete, "/api/v1/compliance/custom-rules/no-greenwashing", nil)
	deleteAgain.Header.Set("X-Tenant-ID", "tenant-b")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, deleteAgain)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing tenant B rule status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	tenantA := listCustomRulesAPI(t, srv, "tenant-a")
	if len(tenantA.Rules) != 1 {
		t.Fatalf("tenant A rules after tenant B delete = %+v", tenantA.Rules)
	}
}

func TestProductEndpointsDoNotLeakAcrossTenantHeaders(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	productA := createProductAPI(t, srv, "tenant-a", "TENANT-A-SKU", "Tenant A Band")
	createProductAPI(t, srv, "tenant-b", "TENANT-B-SKU", "Tenant B Roller")

	listA := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	listA.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, listA)
	if rec.Code != http.StatusOK {
		t.Fatalf("list tenant A status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var listed listResponse
	if err := json.NewDecoder(rec.Body).Decode(&listed); err != nil {
		t.Fatalf("decode tenant A product list: %v", err)
	}
	if listed.Total != 1 || listed.Products[0].SKU != "TENANT-A-SKU" {
		t.Fatalf("tenant A product list = %+v, want only tenant A product", listed)
	}

	crossGet := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+productA.ID, nil)
	crossGet.Header.Set("X-Tenant-ID", "tenant-b")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, crossGet)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant product get status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestProductMutationEndpointsDoNotCrossTenantBoundaries(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	productA := createProductAPI(t, srv, "tenant-a", "TENANT-A-MUTATE", "Tenant A Mutate")

	updateBody := `{"sku":"TENANT-A-MUTATE","title":"Cross Update","description":"Updated by another tenant.","price":{"amount":2995,"currency":"AUD"},"stock":8,"status":"active"}`
	crossUpdate := httptest.NewRequest(http.MethodPut, "/api/v1/products/"+productA.ID, strings.NewReader(updateBody))
	crossUpdate.Header.Set("Content-Type", "application/json")
	crossUpdate.Header.Set("X-Tenant-ID", "tenant-b")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, crossUpdate)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant update status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	crossDelete := httptest.NewRequest(http.MethodDelete, "/api/v1/products/"+productA.ID, nil)
	crossDelete.Header.Set("X-Tenant-ID", "tenant-b")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, crossDelete)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant delete status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}

	deleteOwn := httptest.NewRequest(http.MethodDelete, "/api/v1/products/"+productA.ID, nil)
	deleteOwn.Header.Set("X-Tenant-ID", "tenant-a")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, deleteOwn)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("tenant A delete status = %d, want 204; body=%s", rec.Code, rec.Body.String())
	}
}

func TestOrderEndpointsDoNotLeakAcrossTenantHeaders(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	orderA := createOrderAPI(t, srv, "tenant-a")
	createOrderAPI(t, srv, "tenant-b")

	crossGet := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderA.ID, nil)
	crossGet.Header.Set("X-Tenant-ID", "tenant-b")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, crossGet)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant order get status = %d, want 404; body=%s", rec.Code, rec.Body.String())
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

func TestComplianceReportsExportJSONIsTenantScopedAndSanitized(t *testing.T) {
	t.Parallel()

	srv, productID := seedComplianceReport(t)

	export := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/reports/export?format=json", nil)
	export.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, export)
	if rec.Code != http.StatusOK {
		t.Fatalf("json export status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	for _, forbidden := range []string{"customer_email", "consumer_key", "consumer_secret", "secret_ref", "secret_hash"} {
		if strings.Contains(body, forbidden) {
			t.Fatalf("json export contains sensitive field %q: %s", forbidden, body)
		}
	}
	var records []compliance.EvaluationRecord
	if err := json.NewDecoder(strings.NewReader(body)).Decode(&records); err != nil {
		t.Fatalf("decode json export: %v", err)
	}
	if len(records) != 1 || records[0].TenantID != "tenant-a" || records[0].ProductID != productID {
		t.Fatalf("json records = %+v", records)
	}
}

func TestRAGSearchEndpointUsesTenantContext(t *testing.T) {
	t.Parallel()

	srv, _ := testServer(t)
	srv.rag = rag.NewService(staticRAGEmbedder{}, rag.NewInMemoryVectorStore(2), rag.ChunkOptions{MaxWords: 20})
	for _, doc := range []rag.Document{
		{ID: "doc-a", TenantID: "tenant-a", Content: "tenant a private supplier evidence"},
		{ID: "doc-b", TenantID: "tenant-b", Content: "tenant b private supplier evidence"},
	} {
		if _, err := srv.rag.Ingest(nil, doc); err != nil {
			t.Fatalf("ingest %s: %v", doc.ID, err)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/rag/search?q=supplier&top_k=10", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("rag search status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload ragSearchResponse
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode rag search: %v", err)
	}
	if len(payload.Results) != 1 || payload.Results[0].TenantID != "tenant-a" {
		t.Fatalf("tenant-scoped rag results = %+v, want only tenant-a", payload.Results)
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

func createProductAPI(t *testing.T, srv *server, tenantID, sku, title string) productResponse {
	t.Helper()
	body := fmt.Sprintf(`{"sku":%q,"title":%q,"description":"Tenant scoped product for QA.","price":{"amount":2495,"currency":"AUD"},"stock":10,"status":"active"}`, sku, title)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create product %s status = %d, want 201; body=%s", tenantID, rec.Code, rec.Body.String())
	}
	var created productResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created product: %v", err)
	}
	return created
}

func createOrderAPI(t *testing.T, srv *server, tenantID string) orderResponse {
	t.Helper()
	body := `{"customer_email":"shopper@example.com","delivery_option":"standard","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Tenant-ID", tenantID)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order %s status = %d, want 201; body=%s", tenantID, rec.Code, rec.Body.String())
	}
	var created orderResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode created order: %v", err)
	}
	return created
}

type staticRAGEmbedder struct{}

func (staticRAGEmbedder) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i := range texts {
		out[i] = []float64{1, 0}
	}
	return out, nil
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
	if err := repo.CreateWithTenant(context.Background(), product, "tenant-a"); err != nil {
		t.Fatalf("tag compliance product with tenant: %v", err)
	}
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
