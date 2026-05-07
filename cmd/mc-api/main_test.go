package main

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
)

func testServer(t *testing.T) (*server, *inmemory.ProductRepository) {
	t.Helper()
	repo := inmemory.NewProductRepository()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	srv := &server{
		cfg:       serverConfig{allowedOrigin: "", apiToken: ""},
		repo:      repo,
		orderRepo: inmemory.NewOrderRepository(),
		cartRepo:  inmemory.NewCartRepository(),
		log:       logger,
	}
	return srv, repo
}

func testServerWithCfg(t *testing.T, cfg serverConfig) (*server, *inmemory.ProductRepository) {
	t.Helper()
	repo := inmemory.NewProductRepository()
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	return &server{cfg: cfg, repo: repo, orderRepo: inmemory.NewOrderRepository(), cartRepo: inmemory.NewCartRepository(), log: logger}, repo
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

func TestReadyzReportsSkippedOptionalDependencies(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got readyzResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "ready" {
		t.Fatalf("status = %q, want ready", got.Status)
	}
	if got.Checks["database"].Status != "skipped" || got.Checks["redis"].Status != "skipped" {
		t.Fatalf("dependency checks = %#v", got.Checks)
	}
	if !got.AgentWorker.Ready || got.AgentWorker.RegisteredAgents == 0 {
		t.Fatalf("agent readiness = %#v", got.AgentWorker)
	}
}

func TestReadyzFailsWhenConfiguredDatabaseIsUnavailable(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_URL", "postgres://postgres:postgres@127.0.0.1:1/ecommerce?sslmode=disable")
	t.Setenv("ECOMMERCE_READINESS_TIMEOUT", "10ms")
	repo := inmemory.NewProductRepository()
	srv := newServer(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), repo, inmemory.NewOrderRepository(), inmemory.NewCartRepository())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	var got readyzResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "not_ready" || got.Checks["database"].Status != "fail" {
		t.Fatalf("readiness response = %#v", got)
	}
}

func TestReadyzFailsWhenConfiguredRedisIsUnavailable(t *testing.T) {
	t.Setenv("ECOMMERCE_REDIS_ADDR", "127.0.0.1:1")
	t.Setenv("ECOMMERCE_READINESS_TIMEOUT", "10ms")
	repo := inmemory.NewProductRepository()
	srv := newServer(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), repo, inmemory.NewOrderRepository(), inmemory.NewCartRepository())

	req := httptest.NewRequest(http.MethodGet, "/readyz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body=%s", rec.Code, rec.Body.String())
	}
	var got readyzResponse
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode body: %v", err)
	}
	if got.Status != "not_ready" || got.Checks["redis"].Status != "fail" {
		t.Fatalf("readiness response = %#v", got)
	}
}

func TestNewServerReadsOperationalTimeouts(t *testing.T) {
	t.Setenv("ECOMMERCE_READINESS_TIMEOUT", "75ms")
	t.Setenv("ECOMMERCE_SHUTDOWN_TIMEOUT", "3s")
	repo := inmemory.NewProductRepository()

	srv := newServer(slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil)), repo, inmemory.NewOrderRepository(), inmemory.NewCartRepository())
	defer srv.Close()

	if srv.cfg.readinessTimeout.String() != "75ms" {
		t.Fatalf("readiness timeout = %s, want 75ms", srv.cfg.readinessTimeout)
	}
	if srv.cfg.shutdownTimeout.String() != "3s" {
		t.Fatalf("shutdown timeout = %s, want 3s", srv.cfg.shutdownTimeout)
	}
}

func TestMetricsEndpointExposesPrometheusText(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/plain; version=0.0.4; charset=utf-8" {
		t.Fatalf("content-type = %q", got)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"# TYPE agentic_ecommerce_build_info gauge",
		"# TYPE agentic_ecommerce_sync_lag_seconds gauge",
		"# TYPE agentic_ecommerce_sync_conflicts_total counter",
		"# TYPE agentic_ecommerce_agent_success_total counter",
		"# HELP agentic_ecommerce_compliance_checks_total Compliance checks evaluated by the backend.",
		"# TYPE agentic_ecommerce_compliance_checks_total counter",
		`agentic_ecommerce_compliance_checks_total{source="stub"} 0`,
		"# HELP agentic_ecommerce_compliance_failures_total Compliance checks that failed the publish gate.",
		"# TYPE agentic_ecommerce_compliance_failures_total counter",
		`agentic_ecommerce_compliance_failures_total{source="stub"} 0`,
		"# HELP agentic_ecommerce_media_validation_failures_total Media uploads rejected by validation.",
		"# TYPE agentic_ecommerce_media_validation_failures_total counter",
		`agentic_ecommerce_media_validation_failures_total{reason="stub"} 0`,
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}

func TestSeedDefaultProductsCreatesStorefrontFixtures(t *testing.T) {
	t.Parallel()

	repo := inmemory.NewProductRepository()
	seedDefaultProducts(repo)

	result, err := repo.List(context.Background(), 1, 10)
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if result.Total != 2 {
		t.Fatalf("total = %d, want 2", result.Total)
	}
	if result.Products[0].Status() != catalog.StatusActive || result.Products[1].Status() != catalog.StatusActive {
		t.Fatalf("seeded statuses = %s/%s, want active", result.Products[0].Status(), result.Products[1].Status())
	}
}

func TestRecentEventsReturnsFrontendContract(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	bus := eventbus.NewInMemoryBus()
	srv.eventBus = bus
	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC)
	_ = bus.Publish(context.Background(), eventbus.Event{
		ID:        "evt-product",
		Type:      eventbus.ProductCreated,
		TenantID:  "tenant-a",
		Payload:   map[string]any{"sku": "SKU-1"},
		Timestamp: now,
		Source:    "unit-test",
	})
	_ = bus.Publish(context.Background(), eventbus.Event{
		ID:        "evt-compliance",
		Type:      eventbus.ComplianceChecked,
		TenantID:  "tenant-a",
		Payload:   map[string]any{"passed": false},
		Timestamp: now.Add(time.Minute),
		Source:    "unit-test",
	})

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/recent?limit=1", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []struct {
			ID         string         `json:"id"`
			Type       string         `json:"type"`
			Severity   string         `json:"severity"`
			Message    string         `json:"message"`
			OccurredAt string         `json:"occurred_at"`
			Metadata   map[string]any `json:"metadata"`
		} `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 1 {
		t.Fatalf("events = %d, want 1", len(body.Events))
	}
	got := body.Events[0]
	if got.ID != "evt-compliance" || got.Type != "compliance.checked" || got.Severity != "warning" {
		t.Fatalf("event = %+v", got)
	}
	if got.Message == "" || got.OccurredAt != now.Add(time.Minute).Format(time.RFC3339) {
		t.Fatalf("event message/time = %+v", got)
	}
	if got.Metadata["tenant_id"] != "tenant-a" {
		t.Fatalf("metadata = %+v, want tenant_id tenant-a", got.Metadata)
	}
}

func TestRecentEventsHandlesEmptyBus(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/events/recent", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Events []any `json:"events"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(body.Events) != 0 {
		t.Fatalf("events = %d, want 0", len(body.Events))
	}
}

func TestRequestLoggingAddsRequestIDAndStructuredFields(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	srv, _ := testServer(t)
	srv.log = logger

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("X-Request-ID", "req-test-123")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if got := rec.Header().Get("X-Request-ID"); got != "req-test-123" {
		t.Fatalf("X-Request-ID = %q, want req-test-123", got)
	}

	logLine := logs.String()
	for _, want := range []string{
		`"msg":"http.request"`,
		`"request_id":"req-test-123"`,
		`"method":"GET"`,
		`"path":"/healthz"`,
		`"status":200`,
		`"duration_ms":`,
	} {
		if !strings.Contains(logLine, want) {
			t.Fatalf("log line missing %q:\n%s", want, logLine)
		}
	}
}

func TestRequestLoggingGeneratesRequestID(t *testing.T) {
	t.Parallel()
	var logs bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&logs, nil))
	srv, _ := testServer(t)
	srv.log = logger

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	requestID := rec.Header().Get("X-Request-ID")
	if requestID == "" {
		t.Fatal("expected generated X-Request-ID response header")
	}
	if !strings.Contains(logs.String(), `"request_id":"`+requestID+`"`) {
		t.Fatalf("log line missing generated request ID %q:\n%s", requestID, logs.String())
	}
}

func TestIsHealthcheckArgs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want bool
	}{
		{name: "no args", args: []string{"/app"}, want: false},
		{name: "healthcheck", args: []string{"/app", "healthcheck"}, want: true},
		{name: "flag", args: []string{"/app", "--healthcheck"}, want: true},
		{name: "other", args: []string{"/app", "serve"}, want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := isHealthcheckArgs(tt.args); got != tt.want {
				t.Fatalf("isHealthcheckArgs(%v) = %v, want %v", tt.args, got, tt.want)
			}
		})
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

func TestCreateOrder_Success(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{
		"customer_email":"shopper@example.com",
		"items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":2,"unit_price":{"amount":2495,"currency":"AUD"}}],
		"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}
	}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp orderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.ID == "" || resp.Status != "pending" || resp.Totals.Total.Amount != 4990 {
		t.Fatalf("order response = %+v", resp)
	}
}

func TestGetOrder_ByID(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	orderID := createTestOrder(t, srv)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/orders/"+orderID, nil)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp orderResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.ID != orderID || resp.CustomerEmail != "shopper@example.com" {
		t.Fatalf("order response = %+v", resp)
	}
}

func TestPatchOrderStatus_ValidTransition(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	orderID := createTestOrder(t, srv)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/orders/"+orderID+"/status", bytes.NewBufferString(`{"status":"paid"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp orderResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Status != "paid" {
		t.Fatalf("Status = %q, want paid", resp.Status)
	}
}

func TestPatchOrderStatus_InvalidTransition(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	orderID := createTestOrder(t, srv)

	req := httptest.NewRequest(http.MethodPatch, "/api/v1/orders/"+orderID+"/status", bytes.NewBufferString(`{"status":"fulfilled"}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("status = %d, want 422; body=%s", rec.Code, rec.Body.String())
	}
}

func TestPutAndGetCart(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	body := `{"items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":2,"unit_price":{"amount":2495,"currency":"AUD"}}]}`
	req := httptest.NewRequest(http.MethodPut, "/api/v1/cart/session-123", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("put status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodGet, "/api/v1/cart/session-123", nil)
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var resp cartResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.SessionID != "session-123" || resp.Totals.Total.Amount != 4990 {
		t.Fatalf("cart response = %+v", resp)
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

func TestProductMutationAcceptsLegacyBearerWhenConfigured(t *testing.T) {
	t.Parallel()
	srv, _ := testServerWithCfg(t, serverConfig{apiToken: "test-token"})

	body := `{"sku":"LEGACY-001","title":"Legacy Product","price":{"amount":1000,"currency":"AUD"},"stock":1}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString(body))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("missing token status = %d, want 401", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/v1/products", bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer test-token")
	rec = httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("valid token status = %d, want 201; body=%s", rec.Code, rec.Body.String())
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

func TestQueryDefaultEmbeddingDimensions(t *testing.T) {
	t.Setenv("ECOMMERCE_RAG_EMBEDDING_DIMENSIONS", "16")
	if got := queryDefaultEmbeddingDimensions(); got != 16 {
		t.Fatalf("dimensions = %d, want 16", got)
	}
	t.Setenv("ECOMMERCE_RAG_EMBEDDING_DIMENSIONS", "bad")
	if got := queryDefaultEmbeddingDimensions(); got != 1536 {
		t.Fatalf("fallback dimensions = %d, want 1536", got)
	}
}

func createTestOrder(t *testing.T, srv *server) string {
	t.Helper()
	body := `{"customer_email":"shopper@example.com","items":[{"product_id":"c1000000-0000-0000-0000-000000000001","sku":"BAND-001","title":"Resistance Band","quantity":1,"unit_price":{"amount":2495,"currency":"AUD"}}],"shipping_address":{"name":"Jane Shopper","line1":"1 Market Street","city":"Sydney","region":"NSW","postal_code":"2000","country":"AU"}}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/orders", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create order status = %d, want 201; body=%s", rec.Code, rec.Body.String())
	}
	var resp orderResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode order: %v", err)
	}
	return resp.ID
}
