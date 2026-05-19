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
	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/adapter/woocommerce"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/marketplacesync"
	"github.com/nfsarch33/helixon-ec/internal/security"
	enginesync "github.com/nfsarch33/helixon-ec/internal/sync"
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

func TestSyncStatusEndpointIncludesMarketplaceFields(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/status", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, ok := payload["dlq_depth"].(float64); !ok || got != 0 {
		t.Fatalf("dlq_depth = %#v, want 0", payload["dlq_depth"])
	}
	replay, ok := payload["marketplace_replay"].(map[string]any)
	if !ok {
		t.Fatalf("marketplace_replay = %#v, want object", payload["marketplace_replay"])
	}
	if replay["state"] != "idle" {
		t.Fatalf("replay state = %#v, want idle", replay["state"])
	}
	reconciliation, ok := payload["marketplace_reconciliation"].(map[string]any)
	if !ok {
		t.Fatalf("marketplace_reconciliation = %#v, want object", payload["marketplace_reconciliation"])
	}
	if reconciliation["mismatch_count"] != float64(0) {
		t.Fatalf("mismatch_count = %#v, want 0", reconciliation["mismatch_count"])
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

	body := bytes.NewBufferString(`{"resolution":"local","note":"reviewed by operator"}`)
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

func TestSyncDLQEndpointListsMarketplaceRecords(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/sync/dlq", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var payload map[string]any
	if err := json.NewDecoder(rec.Body).Decode(&payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got, ok := payload["records"].([]any); !ok || len(got) != 0 {
		t.Fatalf("records = %#v, want empty array", payload["records"])
	}
	if got, ok := payload["total"].(float64); !ok || got != 0 {
		t.Fatalf("total = %#v, want 0", payload["total"])
	}
}

func TestReplayMarketplaceDLQReturnsNotFoundForUnknownRecord(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/dlq/missing/replay", nil)
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body=%s", rec.Code, rec.Body.String())
	}
}

func TestReplayMarketplaceDLQRequiresOperatorRole(t *testing.T) {
	t.Parallel()

	srv, _ := testServerWithCfg(t, workflowAuthServerConfig())
	srv.configureSecurity()
	srv.syncEngine = enginesync.NewEngine(enginesync.Config{
		ProductRepository: inmemory.NewProductRepository(),
		WooCommerce:       &syncFakeWooCommerce{},
		DefaultCurrency:   "AUD",
	})
	dlq := marketplacesync.NewInMemoryDLQ()
	if err := dlq.Enqueue(context.Background(), marketplacesync.DLQRecord{
		ID:       "dlq-operator-1",
		Attempts: 3,
		Reason:   "transient timeout",
		Event: marketplacesync.ProductEvent{
			TenantID:   "tenant-a",
			Provider:   "shopify",
			EntityType: marketplacesync.EntityProduct,
			EntityID:   "sku-operator",
			Operation:  marketplacesync.OperationUpsert,
			Version:    "v1",
		},
	}); err != nil {
		t.Fatalf("seed dlq: %v", err)
	}
	router, err := marketplacesync.NewRouter(marketplacesync.RouterConfig{
		Connectors:  map[string]marketplacesync.Connector{},
		Ledger:      marketplacesync.NewInMemoryLedger(),
		DLQ:         dlq,
		MaxAttempts: 3,
	})
	if err != nil {
		t.Fatalf("NewRouter: %v", err)
	}
	srv.marketplaceSync = router
	srv.workflowClient = &fakeTemporalWorkflowClient{run: fakeWorkflowRun{id: "marketplace-replay-dlq-operator-1", runID: "run-marketplace-replay"}}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/dlq/dlq-operator-1/replay", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}

	viewerReq := httptest.NewRequest(http.MethodPost, "/api/v1/sync/dlq/dlq-operator-1/replay", nil)
	viewerReq.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "viewer@example.com", security.RoleViewer))
	viewerRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(viewerRec, viewerReq)
	if viewerRec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403; body=%s", viewerRec.Code, viewerRec.Body.String())
	}

	operatorReq := httptest.NewRequest(http.MethodPost, "/api/v1/sync/dlq/dlq-operator-1/replay", nil)
	operatorReq.Header.Set("Authorization", "Bearer "+mintTestAccessToken(t, srv, "operator@example.com", security.RoleOperator))
	operatorRec := httptest.NewRecorder()
	srv.mux().ServeHTTP(operatorRec, operatorReq)
	if operatorRec.Code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202; body=%s", operatorRec.Code, operatorRec.Body.String())
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

func TestWooCommerceProductWebhookEndpointRejectsInvalidHMAC(t *testing.T) {
	t.Parallel()

	srv, _, _ := testSyncServer(t)
	body := []byte(`{"id":7,"sku":"HOOK-1","name":"Webhook product"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/webhooks/woocommerce/products", bytes.NewReader(body))
	req.Header.Set("X-WC-Webhook-Signature", testWebhookSignature(body, "wrong-secret"))
	rec := httptest.NewRecorder()

	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body=%s", rec.Code, rec.Body.String())
	}
	if got := srv.syncEngine.Status().TotalEvents; got != 0 {
		t.Fatalf("events = %d, want 0", got)
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

func TestSyncAuditActionClassifiesMarketplaceReplay(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/api/v1/sync/dlq/dlq-123/replay", nil)
	got := syncAuditAction(req)
	if got.Action != "sync.marketplace_dlq_replay" || got.Resource != "dlq/dlq-123/replay" || !got.Mutates {
		t.Fatalf("sync audit = %+v", got)
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

func seedSyncConflict(t *testing.T, srv *server, repo *inmemory.ProductRepository, wc *syncFakeWooCommerce) catalog.Product {
	t.Helper()
	product := addProduct(t, repo, "SYNC-CONTRACT-1", "Local Sync Product", 1000)
	wc.products = []woocommerce.Product{{ID: 404, SKU: product.SKU(), Name: "Remote Sync Product", Regular: "12.00"}}
	if _, err := srv.syncEngine.ImportFromWooCommerce(context.Background(), enginesync.ImportOptions{}); err != nil {
		t.Fatalf("seed conflict: %v", err)
	}
	return product
}

func testWebhookSignature(body []byte, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

var _ = uuid.Nil
