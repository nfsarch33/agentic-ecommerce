package woocommerce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func TestClient_UpsertProductSendsWooCommercePayload(t *testing.T) {
	t.Parallel()

	var got map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/wp-json/wc/v3/products" {
			t.Fatalf("path = %s, want products endpoint", r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	t.Cleanup(server.Close)

	price, _ := catalog.NewMoney(4995, "AUD")
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         "band-001",
		Title:       "Resistance Band",
		Description: "Loop band for mobility work",
		Price:       price,
		Stock:       12,
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}

	client := NewClient(Config{BaseURL: server.URL, ConsumerKey: "ck_test", ConsumerSecret: "cs_test"}, server.Client())
	if err := client.UpsertProduct(context.Background(), product); err != nil {
		t.Fatalf("upsert product: %v", err)
	}

	if got["sku"] != "BAND-001" || got["name"] != "Resistance Band" {
		t.Fatalf("payload = %#v", got)
	}
	if got["regular_price"] != "49.95" {
		t.Fatalf("regular_price = %v, want 49.95", got["regular_price"])
	}
}

func TestClient_UpsertProductRejectsHTTPFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	price, _ := catalog.NewMoney(2500, "AUD")
	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:   "BAND-001",
		Title: "Resistance Band",
		Price: price,
	})
	if err != nil {
		t.Fatalf("product: %v", err)
	}

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	if err := client.UpsertProduct(context.Background(), product); err == nil {
		t.Fatal("expected HTTP failure")
	}
}

func TestClient_ListProductsAddsQueryAndDecodesResponse(t *testing.T) {
	t.Parallel()
	fixture, err := os.ReadFile(filepath.Join("testdata", "products_list_response.json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/v3/products" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("per_page"); got != "25" {
			t.Fatalf("per_page = %q, want 25", got)
		}
		if got := r.URL.Query().Get("sku"); got != "BAND-001" {
			t.Fatalf("sku = %q, want BAND-001", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	products, err := client.ListProducts(context.Background(), ListOptions{PerPage: 25, SKU: "BAND-001"})
	if err != nil {
		t.Fatalf("list products: %v", err)
	}
	if len(products) != 1 || products[0].SKU != "BAND-001" || products[0].Name != "Resistance Band" {
		t.Fatalf("products = %+v", products)
	}
}

func TestClient_BatchCreateProductsPostsBatchPayload(t *testing.T) {
	t.Parallel()

	var got map[string][]Product
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/wp-json/wc/v3/products/batch" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode body: %v", err)
		}
		_ = json.NewEncoder(w).Encode(BatchResult{Create: []Product{{ID: 99, SKU: "BATCH-1", Name: "Batch"}}})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	result, err := client.BatchCreateProducts(context.Background(), []Product{{SKU: "BATCH-1", Name: "Batch"}})
	if err != nil {
		t.Fatalf("batch create: %v", err)
	}
	if len(got["create"]) != 1 || got["create"][0].SKU != "BATCH-1" {
		t.Fatalf("body = %+v", got)
	}
	if len(result.Create) != 1 || result.Create[0].ID != 99 {
		t.Fatalf("result = %+v", result)
	}
}

func TestClient_ListOrdersAddsFilters(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/wp-json/wc/v3/orders" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		if got := r.URL.Query().Get("status"); got != "processing" {
			t.Fatalf("status = %q, want processing", got)
		}
		_ = json.NewEncoder(w).Encode([]Order{{ID: 10, Status: "processing", Total: "19.95"}})
	}))
	t.Cleanup(server.Close)

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	orders, err := client.ListOrders(context.Background(), ListOptions{Status: "processing", Page: 2})
	if err != nil {
		t.Fatalf("list orders: %v", err)
	}
	if len(orders) != 1 || orders[0].ID != 10 {
		t.Fatalf("orders = %+v", orders)
	}
}

func TestWcStatus_MapsCorrectly(t *testing.T) {
	t.Parallel()

	tests := []struct {
		status catalog.ProductStatus
		want   string
	}{
		{catalog.StatusActive, "publish"},
		{catalog.StatusDraft, "draft"},
		{catalog.StatusArchived, "private"},
	}
	for _, tc := range tests {
		if got := wcStatus(tc.status); got != tc.want {
			t.Errorf("wcStatus(%q) = %q, want %q", tc.status, got, tc.want)
		}
	}
}
