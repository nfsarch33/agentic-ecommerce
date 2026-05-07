package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHealthz(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var got map[string]string
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got["status"] != "ok" || got["service"] != "agentic-ecommerce-mc-api" {
		t.Fatalf("body = %#v", got)
	}
}

func TestProductsReturnsBFFContract(t *testing.T) {
	t.Setenv("ECOMMERCE_API_TOKEN", "")
	t.Setenv("ECOMMERCE_ALLOWED_ORIGIN", "")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("Content-Type"); got != "application/json" {
		t.Fatalf("content-type = %q, want application/json", got)
	}
	var products []productResponse
	if err := json.NewDecoder(rec.Body).Decode(&products); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if len(products) == 0 {
		t.Fatal("expected seeded products")
	}
	p := products[0]
	if p.ID == "" || p.Title == "" || p.Slug == "" {
		t.Fatalf("missing identity fields: %#v", p)
	}
	if p.Price.Amount <= 0 || p.Price.Currency != "AUD" {
		t.Fatalf("unexpected price: %#v", p.Price)
	}
	if p.Stock < 0 {
		t.Fatalf("stock must be non-negative: %#v", p)
	}
}

func TestProductsRequiresBearerWhenConfigured(t *testing.T) {
	t.Setenv("ECOMMERCE_API_TOKEN", "test-token")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	newMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	newMux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", rec.Code)
	}
}

func TestProductsOPTIONSHandlesCORS(t *testing.T) {
	t.Setenv("ECOMMERCE_ALLOWED_ORIGIN", "https://shop.example")

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://shop.example")
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://shop.example" {
		t.Fatalf("allow-origin = %q", got)
	}
	if got := rec.Header().Get("Access-Control-Allow-Headers"); got != "Authorization, Content-Type" {
		t.Fatalf("allow-headers = %q", got)
	}
}

func TestProductsRejectsDisallowedCORSOrigin(t *testing.T) {
	t.Setenv("ECOMMERCE_ALLOWED_ORIGIN", "https://shop.example")

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()

	newMux().ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestGetenvFallback(t *testing.T) {
	t.Setenv("ECOMMERCE_TEST_EMPTY", "")

	if got := getenv("ECOMMERCE_TEST_EMPTY", "fallback"); got != "fallback" {
		t.Fatalf("getenv fallback = %q", got)
	}
}

func TestGetenvValue(t *testing.T) {
	t.Setenv("ECOMMERCE_TEST_VALUE", "custom")

	if got := getenv("ECOMMERCE_TEST_VALUE", "fallback"); got != "custom" {
		t.Fatalf("getenv value = %q", got)
	}
}
