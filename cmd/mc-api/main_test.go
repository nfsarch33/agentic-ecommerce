package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func testServer(t *testing.T) (*server, *inmemory.ProductRepository) {
	t.Helper()
	repo := inmemory.NewProductRepository()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := &server{
		cfg:  serverConfig{allowedOrigin: "", apiToken: ""},
		repo: repo,
		log:  logger,
	}
	return srv, repo
}

func testServerWithCfg(t *testing.T, cfg serverConfig) (*server, *inmemory.ProductRepository) {
	t.Helper()
	repo := inmemory.NewProductRepository()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return &server{cfg: cfg, repo: repo, log: logger}, repo
}

func addProduct(t *testing.T, repo *inmemory.ProductRepository, sku, title string, amount int) catalog.Product {
	t.Helper()
	price, _ := catalog.NewMoney(amount, "AUD")
	p, err := catalog.NewProduct(catalog.ProductInput{
		SKU:    sku,
		Title:  title,
		Price:  price,
		Stock:  10,
		Status: catalog.StatusActive,
	})
	if err != nil {
		t.Fatalf("create product: %v", err)
	}
	if err := repo.Create(context.Background(), p); err != nil {
		t.Fatalf("repo create: %v", err)
	}
	return p
}

func TestHealthz(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

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

func TestListProducts_Empty(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp listResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 0 || len(resp.Products) != 0 {
		t.Fatalf("expected empty list, got total=%d products=%d", resp.Total, len(resp.Products))
	}
}

func TestListProducts_WithPagination(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)

	for i := 0; i < 5; i++ {
		addProduct(t, repo, "SKU-"+string(rune('A'+i)), "Product "+string(rune('A'+i)), 1000+i*100)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products?page=1&per_page=2", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}

	var resp listResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Total != 5 {
		t.Fatalf("Total = %d, want 5", resp.Total)
	}
	if len(resp.Products) != 2 {
		t.Fatalf("len = %d, want 2", len(resp.Products))
	}
	if resp.Page != 1 || resp.PerPage != 2 {
		t.Fatalf("pagination = page=%d per_page=%d", resp.Page, resp.PerPage)
	}
}

func TestCreateProduct_Success(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{"sku":"BAND-001","title":"Resistance Band","price":{"amount":4995,"currency":"AUD"},"stock":12}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}

	var resp productResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.SKU != "BAND-001" {
		t.Fatalf("SKU = %q, want BAND-001", resp.SKU)
	}
	if resp.Price.Amount != 4995 || resp.Price.Currency != "AUD" {
		t.Fatalf("Price = %+v", resp.Price)
	}
	if resp.Status != "draft" {
		t.Fatalf("Status = %q, want draft", resp.Status)
	}
	if resp.ID == "" {
		t.Fatal("expected non-empty ID")
	}
}

func TestCreateProduct_InvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString("{bad"))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestCreateProduct_MissingSKU(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{"title":"Resistance Band","price":{"amount":4995,"currency":"AUD"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetProduct_ByID(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "BAND-001", "Resistance Band", 4995)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+p.ID().String(), nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp productResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.SKU != "BAND-001" {
		t.Fatalf("SKU = %q", resp.SKU)
	}
}

func TestGetProduct_BySlug(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "BAND-001", "Resistance Band", 4995)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/"+p.Slug(), nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

func TestGetProduct_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/nonexistent-slug", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestUpdateProduct_Success(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "BAND-001", "Resistance Band", 4995)

	body := `{"sku":"BAND-001","title":"Updated Band","price":{"amount":5500,"currency":"AUD"},"stock":20,"status":"active"}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/products/"+p.ID().String(), bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	var resp productResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Title != "Updated Band" {
		t.Fatalf("Title = %q", resp.Title)
	}
	if resp.Price.Amount != 5500 {
		t.Fatalf("Price.Amount = %d", resp.Price.Amount)
	}
}

func TestUpdateProduct_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{"sku":"X","title":"X","price":{"amount":100,"currency":"AUD"}}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/products/00000000-0000-0000-0000-000000000000", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestDeleteProduct_Success(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "BAND-001", "Resistance Band", 4995)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/products/"+p.ID().String(), nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
}

func TestDeleteProduct_NotFound(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodDelete, "/api/v1/products/00000000-0000-0000-0000-000000000000", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestProductsRequiresBearerWhenConfigured(t *testing.T) {
	t.Parallel()
	srv, _ := testServerWithCfg(t, serverConfig{apiToken: "test-token"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("valid token status = %d, want 200", rec.Code)
	}
}

func TestProductsOPTIONSHandlesCORS(t *testing.T) {
	t.Parallel()
	srv, _ := testServerWithCfg(t, serverConfig{allowedOrigin: "https://shop.example"})

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://shop.example")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204", rec.Code)
	}
	if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "https://shop.example" {
		t.Fatalf("allow-origin = %q", got)
	}
}

func TestProductsRejectsDisallowedCORSOrigin(t *testing.T) {
	t.Parallel()
	srv, _ := testServerWithCfg(t, serverConfig{allowedOrigin: "https://shop.example"})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

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
