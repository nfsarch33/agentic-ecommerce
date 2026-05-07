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
