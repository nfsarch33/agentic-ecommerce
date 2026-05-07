package main

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/woocommerce"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	enginesync "github.com/nfsarch33/agentic-ecommerce/internal/sync"
)

type syncFakeWooCommerce struct {
	products []woocommerce.Product
	upserts  []catalog.Product
}

func (f *syncFakeWooCommerce) ListProducts(context.Context, woocommerce.ListOptions) ([]woocommerce.Product, error) {
	return f.products, nil
}

func (f *syncFakeWooCommerce) UpsertProduct(_ context.Context, product catalog.Product) error {
	f.upserts = append(f.upserts, product)
	return nil
}

func TestSyncStatusEndpointReportsConflictCounts(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got syncStatusResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.PendingConflicts != 0 {
		t.Fatalf("pending conflicts = %d, want 0", got.PendingConflicts)
	}
}

func TestPublishProductEndpointPublishesToWooCommerce(t *testing.T) {
	t.Parallel()

	srv, repo, wc := testSyncServer(t)
	product := addProduct(t, repo, "PUB-1", "Publishable", 1995)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/products/"+product.ID().String()+"/publish", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if len(wc.upserts) != 1 || wc.upserts[0].ID() != product.ID() {
		t.Fatalf("upserts = %+v", wc.upserts)
	}
}

func TestConflictEndpointsListAndResolveManualReview(t *testing.T) {
	t.Parallel()

	srv, repo, wc := testSyncServer(t)
	local := addProduct(t, repo, "CONFLICT-1", "Local", 1000)
	wc.products = []woocommerce.Product{{ID: 44, SKU: local.SKU(), Name: "Remote", Regular: "12.00"}}
	if _, err := srv.syncEngine.ImportFromWooCommerce(context.Background(), enginesync.ImportOptions{}); err != nil {
		t.Fatalf("import: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/conflicts", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var list conflictsResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode conflicts: %v", err)
	}
	if len(list.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v", list.Conflicts)
	}

	body := bytes.NewBufferString(`{"resolution":"accept_local","note":"reviewed by operator"}`)
	req = httptest.NewRequest(http.MethodPost, "/api/v1/sync/conflicts/"+list.Conflicts[0].ID+"/resolve", body)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("resolve status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if pending := srv.syncEngine.Conflicts(); len(pending) != 0 {
		t.Fatalf("pending conflicts = %+v", pending)
	}
}

func TestWooCommerceProductWebhookEndpointVerifiesHMAC(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	body := []byte(`{"id":7,"sku":"HOOK-1","name":"Webhook product"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/woocommerce/products", bytes.NewReader(body))
	req.Header.Set("X-WC-Webhook-Signature", testWebhookSignature(body, "secret"))
	req.Header.Set("X-WC-Webhook-Topic", "product.updated")
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if got := srv.syncEngine.Status().TotalEvents; got != 1 {
		t.Fatalf("events = %d, want 1", got)
	}
}

func TestPublishProductEndpointRejectsInvalidID(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/products/not-a-uuid/publish", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func testSyncServer(t *testing.T) (*server, *inmemory.ProductRepository, *syncFakeWooCommerce) {
	t.Helper()
	repo := inmemory.NewProductRepository()
	wc := &syncFakeWooCommerce{}
	engine := enginesync.NewEngine(enginesync.Config{ProductRepository: repo, WooCommerce: wc, DefaultCurrency: "AUD"})
	srv := &server{
		cfg:           serverConfig{allowedOrigin: "", apiToken: "", webhookSecret: "secret"},
		repo:          repo,
		orderRepo:     inmemory.NewOrderRepository(),
		cartRepo:      inmemory.NewCartRepository(),
		syncEngine:    engine,
		webhookSecret: "secret",
		log:           slog.New(slog.NewJSONHandler(os.Stdout, nil)),
	}
	return srv, repo, wc
}

func testWebhookSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

var _ = uuid.Nil
