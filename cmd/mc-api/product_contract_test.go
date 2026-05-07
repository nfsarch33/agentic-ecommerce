package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"gopkg.in/yaml.v3"
)

type openAPIContract struct {
	Paths      map[string]openAPIPath `yaml:"paths"`
	Components struct {
		Schemas map[string]openAPISchema `yaml:"schemas"`
	} `yaml:"components"`
}

type openAPIPath struct {
	Get   openAPIOperation `yaml:"get"`
	Post  openAPIOperation `yaml:"post"`
	Put   openAPIOperation `yaml:"put"`
	Patch openAPIOperation `yaml:"patch"`
}

type openAPIOperation struct {
	Responses map[string]openAPIResponse `yaml:"responses"`
}

type openAPIResponse struct {
	Content map[string]openAPIMediaType `yaml:"content"`
}

type openAPIMediaType struct {
	Schema openAPISchema `yaml:"schema"`
}

type openAPISchema struct {
	Ref        string                   `yaml:"$ref"`
	Type       string                   `yaml:"type"`
	Enum       []string                 `yaml:"enum"`
	Required   []string                 `yaml:"required"`
	Properties map[string]openAPISchema `yaml:"properties"`
	Items      *openAPISchema           `yaml:"items"`
}

func TestProductHandlersMatchOpenAPIContract(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)
	srv, repo := testServer(t)
	product := fixedProduct(t)
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("repo create: %v", err)
	}

	t.Run("list products", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}

		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		schema := responseSchema(t, spec, "/api/v1/products", http.MethodGet, "200")
		assertSchemaRequiredFields(t, spec, schema, payload)

		products, ok := payload["products"].([]any)
		if !ok || len(products) != 1 {
			t.Fatalf("products = %#v, want one product array item", payload["products"])
		}
		first, ok := products[0].(map[string]any)
		if !ok {
			t.Fatalf("first product = %#v, want object", products[0])
		}
		productSchema := dereferenceSchema(t, spec, schema.Properties["products"].Items.Ref)
		assertSchemaRequiredFields(t, spec, productSchema, first)
	})

	t.Run("create product", func(t *testing.T) {
		body := `{"sku":"MAT-001","title":"Yoga Mat","price":{"amount":5495,"currency":"AUD"},"stock":40,"status":"active"}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
		}

		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		schema := responseSchema(t, spec, "/api/v1/products", http.MethodPost, "201")
		assertSchemaRequiredFields(t, spec, schema, payload)
	})
}

func TestProductResponseGoldenShape(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	product := fixedProduct(t)
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("repo create: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+product.Slug(), nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	assertGoldenJSON(t, filepath.Join("testdata", "product_response.golden.json"), rec.Body.Bytes())
}

func TestOrderHandlersMatchOpenAPIContract(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)
	srv, _ := testServer(t)

	body := `{"customer_email":"shopper@example.com","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var created map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &created)
	assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/orders", http.MethodPost, "201"), created)

	id, _ := created["id"].(string)
	req = httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+id, nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &got)
	assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/orders/{id}", http.MethodGet, "200"), got)

	req = httptest.NewRequest(http.MethodPatch, "/api/v1/orders/"+id+"/status", bytes.NewBufferString(`{"status":"paid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var patched map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &patched)
	assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/orders/{id}/status", http.MethodPatch, "200"), patched)
}

func TestOrderResponseGoldenShape(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{"customer_email":"shopper@example.com","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	var payload map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &payload)
	payload["id"] = "00000000-0000-0000-0000-000000000000"
	payload["created_at"] = "2026-05-07T00:00:00Z"
	payload["updated_at"] = "2026-05-07T00:00:00Z"
	assertGoldenJSONPayload(t, filepath.Join("testdata", "order_response.golden.json"), payload)
}

func TestCartHandlersMatchOpenAPIContract(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)
	srv, _ := testServer(t)

	body := `{"items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":2,"unit_price":{"amount":2495,"currency":"AUD"}}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cart/session-123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	decodeJSONPayload(t, rec.Body.Bytes(), &payload)
	assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/cart/{session_id}", http.MethodPut, "200"), payload)
}

func TestSyncHandlersMatchOpenAPIContract(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)
	srv, repo, wc := testSyncServer(t)
	product := seedSyncConflict(t, srv, repo, wc)

	t.Run("status", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/sync/status", http.MethodGet, "200"), payload)
	})

	t.Run("conflicts", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/conflicts", nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		schema := responseSchema(t, spec, "/api/v1/sync/conflicts", http.MethodGet, "200")
		assertSchemaRequiredFields(t, spec, schema, payload)
		conflicts, ok := payload["conflicts"].([]any)
		if !ok || len(conflicts) != 1 {
			t.Fatalf("conflicts = %#v, want one conflict", payload["conflicts"])
		}
		first, ok := conflicts[0].(map[string]any)
		if !ok {
			t.Fatalf("conflict = %#v, want object", conflicts[0])
		}
		conflictSchema := dereferenceSchema(t, spec, schema.Properties["conflicts"].Items.Ref)
		assertSchemaRequiredFields(t, spec, conflictSchema, first)
	})

	t.Run("publish", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/products/"+product.ID().String()+"/publish", nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/sync/products/{id}/publish", http.MethodPost, "200"), payload)
	})

	t.Run("resolve", func(t *testing.T) {
		conflictID := srv.syncEngine.Conflicts()[0].ID
		req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/conflicts/"+conflictID+"/resolve", bytes.NewBufferString(`{"resolution":"manual","note":"contract test"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/sync/conflicts/{id}/resolve", http.MethodPost, "200"), payload)
	})
}

func TestContentHandlersMatchOpenAPIContract(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)
	srv, repo := testServer(t)
	product := fixedProduct(t)
	if err := repo.Create(context.Background(), product); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	srv.contentAgent = &fakeContentAgent{result: content.GenerateResult{
		GeneratedContent: content.GeneratedContent{
			Description:     "Resistance Band Set supports progressive home workouts.",
			SEOTitle:        "Resistance Band Set for Home Workouts",
			MetaDescription: "Shop a resistance band set for progressive home workouts.",
		},
		Evaluation: content.Evaluation{Score: 88, Pass: true},
		TokensUsed: 77,
	}}

	t.Run("generate description", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/generate-description", bytes.NewBufferString(`{"style":"casual"}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/products/{id}/generate-description", http.MethodPost, "200"), payload)
	})

	t.Run("ai suggestions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+product.ID().String()+"/ai-suggestions", nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/products/{id}/ai-suggestions", http.MethodGet, "200"), payload)
	})
}

func TestComplianceHandlersMatchOpenAPIContract(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)
	srv, repo := testServer(t)
	product := addProductWithContent(t, repo, catalog.ProductInput{
		SKU:         "RB-SET-5",
		Title:       "Premium Resistance Band Set for Home Workouts",
		Description: "Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training. Includes handles, anchors, and a carry bag for daily training.",
		Images:      []catalog.Image{{URL: "https://cdn.example.com/rb.jpg", Alt: "Premium resistance band set with handles"}},
	})

	t.Run("compliance check", func(t *testing.T) {
		body := `{"keywords":["resistance band set","home workouts"],"seo_title":"Premium Resistance Band Set for Home Workouts","meta_description":"Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training.","seo_score_min":70}`
		req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/compliance-check", bytes.NewBufferString(body))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		schema := responseSchema(t, spec, "/api/v1/products/{id}/compliance-check", http.MethodPost, "200")
		assertSchemaRequiredFields(t, spec, schema, payload)
		results, ok := payload["results"].([]any)
		if !ok || len(results) == 0 {
			t.Fatalf("results = %#v, want per-rule results", payload["results"])
		}
		first, ok := results[0].(map[string]any)
		if !ok {
			t.Fatalf("first result = %#v, want object", results[0])
		}
		resultSchema := dereferenceSchema(t, spec, schema.Properties["results"].Items.Ref)
		assertSchemaRequiredFields(t, spec, resultSchema, first)
	})

	t.Run("seo suggestions", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodPost, "/api/v1/products/"+product.ID().String()+"/seo-suggestions", bytes.NewBufferString(`{"keywords":["resistance band set","home workouts"]}`))
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		assertSchemaRequiredFields(t, spec, responseSchema(t, spec, "/api/v1/products/{id}/seo-suggestions", http.MethodPost, "200"), payload)
	})

	t.Run("rules", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/compliance/rules", nil)
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
		}
		var payload map[string]any
		decodeJSONPayload(t, rec.Body.Bytes(), &payload)
		schema := responseSchema(t, spec, "/api/v1/compliance/rules", http.MethodGet, "200")
		assertSchemaRequiredFields(t, spec, schema, payload)
		rules, ok := payload["rules"].([]any)
		if !ok || len(rules) == 0 {
			t.Fatalf("rules = %#v, want built-in rules", payload["rules"])
		}
		first, ok := rules[0].(map[string]any)
		if !ok {
			t.Fatalf("first rule = %#v, want object", rules[0])
		}
		ruleSchema := dereferenceSchema(t, spec, schema.Properties["rules"].Items.Ref)
		assertSchemaRequiredFields(t, spec, ruleSchema, first)
	})
}

func TestOpenAPIIncludesContentAgentEndpoints(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)

	suggestion := responseSchema(t, spec, "/api/v1/products/{id}/generate-description", http.MethodPost, "200")
	for _, field := range []string{
		"product_id",
		"description",
		"seo_title",
		"meta_description",
		"score",
		"pass",
		"tokens_used",
		"evaluation",
	} {
		if !containsString(suggestion.Required, field) {
			t.Fatalf("ContentSuggestion required fields = %v, want %q", suggestion.Required, field)
		}
	}

	evaluationRef := suggestion.Properties["evaluation"].Ref
	evaluation := dereferenceSchema(t, spec, evaluationRef)
	for _, field := range []string{
		"score",
		"pass",
		"readability_score",
		"keyword_density",
		"tone",
		"length",
		"factual_issues",
	} {
		if !containsString(evaluation.Required, field) {
			t.Fatalf("ContentEvaluation required fields = %v, want %q", evaluation.Required, field)
		}
	}

	_ = responseSchema(t, spec, "/api/v1/products/{id}/ai-suggestions", http.MethodGet, "200")
}

func TestOpenAPIIncludesComplianceEndpointSchemas(t *testing.T) {
	t.Parallel()
	spec := loadOpenAPIContract(t)

	check := responseSchema(t, spec, "/api/v1/products/{id}/compliance-check", http.MethodPost, "200")
	for _, field := range []string{"product_id", "pass", "score", "reasons", "rule_ids", "severity", "results"} {
		if !containsString(check.Required, field) {
			t.Fatalf("ComplianceCheckResponse required fields = %v, want %q", check.Required, field)
		}
	}

	ruleResult := dereferenceSchema(t, spec, check.Properties["results"].Items.Ref)
	for _, field := range []string{"id", "pass", "score", "severity", "reasons"} {
		if !containsString(ruleResult.Required, field) {
			t.Fatalf("ComplianceRuleResult required fields = %v, want %q", ruleResult.Required, field)
		}
	}

	severity := dereferenceSchema(t, spec, ruleResult.Properties["severity"].Ref)
	for _, value := range []string{"info", "warning", "error", "critical"} {
		if !containsString(severity.Enum, value) {
			t.Fatalf("ComplianceSeverity enum = %v, want %q", severity.Enum, value)
		}
	}

	seoSuggestion := responseSchema(t, spec, "/api/v1/products/{id}/seo-suggestions", http.MethodPost, "200")
	for _, field := range []string{"product_id", "title", "meta_description", "slug", "score", "keyword_density", "pass", "reasons"} {
		if !containsString(seoSuggestion.Required, field) {
			t.Fatalf("SEOSuggestionResponse required fields = %v, want %q", seoSuggestion.Required, field)
		}
	}
	_ = responseSchema(t, spec, "/api/v1/compliance/rules", http.MethodGet, "200")
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func fixedProduct(t *testing.T) catalog.Product {
	t.Helper()
	price, err := catalog.NewMoney(4995, "AUD")
	if err != nil {
		t.Fatalf("money: %v", err)
	}
	timestamp := time.Date(2026, 5, 7, 0, 0, 0, 0, time.UTC)
	return catalog.ReconstructProduct(catalog.ProductRecord{
		ID:          uuid.MustParse("b1000000-0000-0000-0000-000000000001"),
		SKU:         "RB-SET-5",
		Title:       "Resistance Band Set",
		Slug:        "resistance-band-set",
		Description: "Progressive resistance band set with 5 tension levels.",
		Price:       price,
		Stock:       120,
		Status:      catalog.StatusActive,
		CreatedAt:   timestamp,
		UpdatedAt:   timestamp,
	})
}

func loadOpenAPIContract(t *testing.T) openAPIContract {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join("..", "..", "api", "openapi.yaml"))
	if err != nil {
		t.Fatalf("read openapi: %v", err)
	}
	var spec openAPIContract
	if err := yaml.Unmarshal(raw, &spec); err != nil {
		t.Fatalf("parse openapi: %v", err)
	}
	return spec
}

func responseSchema(t *testing.T, spec openAPIContract, path, method, status string) openAPISchema {
	t.Helper()
	item, ok := spec.Paths[path]
	if !ok {
		t.Fatalf("missing OpenAPI path %s", path)
	}
	var operation openAPIOperation
	switch method {
	case http.MethodGet:
		operation = item.Get
	case http.MethodPost:
		operation = item.Post
	case http.MethodPut:
		operation = item.Put
	case http.MethodPatch:
		operation = item.Patch
	default:
		t.Fatalf("unsupported method %s", method)
	}
	response, ok := operation.Responses[status]
	if !ok {
		t.Fatalf("missing OpenAPI response %s %s %s", method, path, status)
	}
	media, ok := response.Content["application/json"]
	if !ok {
		t.Fatalf("missing application/json response for %s %s %s", method, path, status)
	}
	return dereferenceSchema(t, spec, media.Schema.Ref)
}

func dereferenceSchema(t *testing.T, spec openAPIContract, ref string) openAPISchema {
	t.Helper()
	const prefix = "#/components/schemas/"
	if len(ref) <= len(prefix) || ref[:len(prefix)] != prefix {
		t.Fatalf("unsupported schema ref %q", ref)
	}
	name := ref[len(prefix):]
	schema, ok := spec.Components.Schemas[name]
	if !ok {
		t.Fatalf("missing schema %s", name)
	}
	return schema
}

func assertSchemaRequiredFields(t *testing.T, spec openAPIContract, schema openAPISchema, payload map[string]any) {
	t.Helper()
	for _, field := range schema.Required {
		if _, ok := payload[field]; !ok {
			t.Fatalf("missing required OpenAPI field %q in %#v", field, payload)
		}
	}
	for name, property := range schema.Properties {
		if property.Ref == "" {
			continue
		}
		nested, ok := payload[name].(map[string]any)
		if !ok {
			continue
		}
		assertSchemaRequiredFields(t, spec, dereferenceSchema(t, spec, property.Ref), nested)
	}
}

func decodeJSONPayload(t *testing.T, raw []byte, out any) {
	t.Helper()
	if err := json.Unmarshal(raw, out); err != nil {
		t.Fatalf("decode JSON payload: %v\n%s", err, raw)
	}
}

func assertGoldenJSON(t *testing.T, goldenPath string, actual []byte) {
	t.Helper()
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden JSON: %v", err)
	}
	var wantPayload, actualPayload any
	decodeJSONPayload(t, want, &wantPayload)
	decodeJSONPayload(t, actual, &actualPayload)
	if !reflect.DeepEqual(wantPayload, actualPayload) {
		var pretty bytes.Buffer
		_ = json.Indent(&pretty, actual, "", "  ")
		pretty.WriteByte('\n')
		t.Fatalf("golden JSON mismatch\nwant:\n%s\ngot:\n%s", want, pretty.Bytes())
	}
}

func assertGoldenJSONPayload(t *testing.T, goldenPath string, actualPayload any) {
	t.Helper()
	actual, err := json.Marshal(actualPayload)
	if err != nil {
		t.Fatalf("marshal actual payload: %v", err)
	}
	assertGoldenJSON(t, goldenPath, actual)
}
