package woocommerce

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
)

func TestClient_UpsertProductSendsWooCommercePayload(t *testing.T) {
	t.Parallel()

	var got map[string]string
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

	product, err := catalog.NewProduct(catalog.ProductInput{
		SKU:         "band-001",
		Title:       "Resistance Band",
		Description: "Loop band for mobility work",
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
}

func TestClient_UpsertProductRejectsHTTPFailure(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	t.Cleanup(server.Close)

	product, err := catalog.NewProduct(catalog.ProductInput{SKU: "BAND-001", Title: "Resistance Band"})
	if err != nil {
		t.Fatalf("product: %v", err)
	}

	client := NewClient(Config{BaseURL: server.URL}, server.Client())
	if err := client.UpsertProduct(context.Background(), product); err == nil {
		t.Fatal("expected HTTP failure")
	}
}
