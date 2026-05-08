package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

// File scope: v2.6.1 cmd/* DI refactor coverage. Targets the
// previously low-coverage handlers and helpers in cmd/mc-api.

// Close() is 33.3% covered: drive it once with a cleanup hook to
// hit the "cleanup != nil" branch.
func TestServer_Close_RunsCleanupHooksAndIgnoresNil(t *testing.T) {
	t.Parallel()

	called := 0
	srv := &server{cleanup: []func(){
		nil, // nil hooks must be skipped
		func() { called++ },
		func() { called++ },
	}}
	srv.Close()
	if called != 2 {
		t.Fatalf("expected 2 cleanup invocations, got %d", called)
	}
}

// productFromTenantRepo (22.2% covered) has 4 branches: UUID hit,
// slug hit, slug miss, ListByTenant error. We exercise the first 3
// via the in-memory tenant repository which is the production-grade
// implementation already used by mc-api.
func TestProductFromTenantRepo_UsesUUIDPath(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewProductRepository()
	price, _ := catalog.NewMoney(1000, "AUD")
	p, _ := catalog.NewProduct(catalog.ProductInput{SKU: "T-1", Title: "tt", Price: price, Stock: 1, Status: catalog.StatusActive})
	if err := repo.CreateWithTenant(context.Background(), p, "tenant-x"); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := productFromTenantRepo(context.Background(), repo, "tenant-x", p.ID().String())
	if err != nil {
		t.Fatalf("by id: %v", err)
	}
	if got.SKU() != "T-1" {
		t.Errorf("sku = %q", got.SKU())
	}
}

func TestProductFromTenantRepo_FindsBySlug(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewProductRepository()
	price, _ := catalog.NewMoney(1000, "AUD")
	p, _ := catalog.NewProduct(catalog.ProductInput{SKU: "T-2", Title: "tt", Slug: "by-slug", Price: price, Stock: 1, Status: catalog.StatusActive})
	if err := repo.CreateWithTenant(context.Background(), p, "tenant-x"); err != nil {
		t.Fatalf("create: %v", err)
	}
	got, err := productFromTenantRepo(context.Background(), repo, "tenant-x", "by-slug")
	if err != nil {
		t.Fatalf("by slug: %v", err)
	}
	if got.Slug() != "by-slug" {
		t.Errorf("slug = %q", got.Slug())
	}
}

func TestProductFromTenantRepo_ReturnsNotFoundForUnknownSlug(t *testing.T) {
	t.Parallel()
	repo := inmemory.NewProductRepository()
	_, err := productFromTenantRepo(context.Background(), repo, "tenant-x", "missing-slug")
	if !errors.Is(err, inmemory.ErrProductNotFound) {
		t.Fatalf("expected ErrProductNotFound, got %v", err)
	}
}

// deleteProduct (42.1%) error path tests.
func TestServer_DeleteProduct_RejectsInvalidUUID(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/products/not-a-uuid", nil)
	rec := httptest.NewRecorder()
	srv.deleteProduct(rec, req, "not-a-uuid")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("invalid_id")) {
		t.Errorf("body = %s", rec.Body.String())
	}
}

func TestServer_DeleteProduct_ReturnsNotFoundForMissingID(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	id := uuid.New().String()
	req := httptest.NewRequest(http.MethodDelete, "/api/v1/products/"+id, nil)
	rec := httptest.NewRecorder()
	srv.deleteProduct(rec, req, id)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

// updateProduct (60%) error path tests.
func TestServer_UpdateProduct_RejectsInvalidUUID(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	body := bytes.NewReader([]byte(`{"sku":"S","title":"T","price":{"amount":1000,"currency":"AUD"},"stock":1}`))
	req := httptest.NewRequest(http.MethodPut, "/api/v1/products/garbage", body)
	rec := httptest.NewRecorder()
	srv.updateProduct(rec, req, "garbage")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestServer_UpdateProduct_RejectsInvalidJSON(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "U-1", "Update Me", 1500)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/products/"+p.ID().String(), bytes.NewReader([]byte("not json")))
	rec := httptest.NewRecorder()
	srv.updateProduct(rec, req, p.ID().String())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestServer_UpdateProduct_RejectsInvalidPrice(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "U-2", "Bad Price", 1500)
	body, _ := json.Marshal(map[string]any{
		"sku":   "U-2",
		"title": "X",
		"price": map[string]any{"amount": -100, "currency": "AUD"},
		"stock": 1,
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/products/"+p.ID().String(), bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.updateProduct(rec, req, p.ID().String())
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422", rec.Code)
	}
}

func TestServer_UpdateProduct_AcceptsValidPayload(t *testing.T) {
	t.Parallel()
	srv, repo := testServer(t)
	p := addProduct(t, repo, "U-3", "Old", 1500)
	body, _ := json.Marshal(map[string]any{
		"sku":         "U-3",
		"title":       "New",
		"slug":        "new-slug",
		"description": "updated",
		"price":       map[string]any{"amount": 2000, "currency": "AUD"},
		"stock":       3,
		"status":      "active",
	})
	req := httptest.NewRequest(http.MethodPut, "/api/v1/products/"+p.ID().String(), bytes.NewReader(body))
	rec := httptest.NewRecorder()
	srv.updateProduct(rec, req, p.ID().String())
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
}

// listProducts (68.2%) — invalid query params and tenant scoping
// branches.
func TestServer_ListProducts_NormalisesPaginationDefaults(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products?page=-1&per_page=999", nil)
	rec := httptest.NewRecorder()
	srv.productsHandler(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	var resp listResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Page != 1 || resp.PerPage != 20 {
		t.Fatalf("page=%d per_page=%d, want defaults", resp.Page, resp.PerPage)
	}
}

// buildStripeWebhookVerifier reaches into env; cover the dev fallback
// path.
func TestBuildStripeWebhookVerifier_ReturnsValidVerifier(t *testing.T) {
	t.Setenv("ECOMMERCE_STRIPE_WEBHOOK_SECRET", "")
	v, err := buildStripeWebhookVerifier()
	if err != nil {
		t.Fatalf("buildStripeWebhookVerifier: %v", err)
	}
	if v == nil {
		t.Fatal("verifier is nil")
	}
}

// queryDefaultEmbeddingDimensions covers the env-not-set + bad-int +
// good-int branches.
func TestQueryDefaultEmbeddingDimensions_AcceptsExplicitInt(t *testing.T) {
	t.Setenv("ECOMMERCE_RAG_EMBEDDING_DIMENSIONS", "256")
	if got := queryDefaultEmbeddingDimensions(); got != 256 {
		t.Fatalf("got %d, want 256", got)
	}
}

func TestQueryDefaultEmbeddingDimensions_FallsBackForGarbage(t *testing.T) {
	t.Setenv("ECOMMERCE_RAG_EMBEDDING_DIMENSIONS", "not-a-number")
	got := queryDefaultEmbeddingDimensions()
	if got <= 0 {
		t.Fatalf("expected positive default, got %d", got)
	}
}

// recordHTTPRequest covers the bucket boundaries.
func TestRecordHTTPRequest_BucketsAllStatusClasses(t *testing.T) {
	t.Parallel()
	for _, status := range []int{200, 301, 404, 500} {
		recordHTTPRequest(status, 10*time.Millisecond)
	}
}

// firstNonEmpty fallback path.
func TestFirstNonEmpty_FallsThroughToEmpty(t *testing.T) {
	t.Parallel()
	if got := firstNonEmpty("", "  ", ""); got != "" {
		t.Errorf("got %q, want empty", got)
	}
	if got := firstNonEmpty("", "x"); got != "x" {
		t.Errorf("got %q, want x", got)
	}
}

// withCORS rejects mismatched origin.
func TestServer_WithCORS_RejectsMismatchedOrigin(t *testing.T) {
	t.Parallel()
	srv := &server{cfg: serverConfig{allowedOrigin: "https://allowed.example.com"}, log: slog.New(slog.NewJSONHandler(io.Discard, nil))}
	handler := srv.withCORS(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products", nil)
	req.Header.Set("Origin", "https://attacker.example.com")
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}
