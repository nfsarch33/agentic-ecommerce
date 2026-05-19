package shopee

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/marketplacesync"
)

func TestClientApplyBuildsSignedShopeeProductRequest(t *testing.T) {
	t.Parallel()

	var got productRequest
	server := productServer(t, &got, mustFixture(t, "product_upsert_success.json"))
	client := mustServerClient(t, server, fixedClock(1777777777))

	result, err := client.Apply(context.Background(), productEvent())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	requireApplyResult(t, result, "987654321")
	requireSignedRequest(t, server, got.RawQuery, "/api/v2/product/add_item")
	requireProductPayload(t, got.Body)
}

func TestClientApplyUsesExistingShopeeItemIDWhenExternalIDIsPresent(t *testing.T) {
	t.Parallel()

	var got productRequest
	server := productServer(t, &got, []byte(`{"error":"","message":"","response":{"item_id":222333444}}`))
	client := mustServerClient(t, server, fixedClock(1777777799))
	event := productEvent()
	event.ExternalID = "222333444"

	if _, err := client.Apply(context.Background(), event); err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if got.Path != "/api/v2/product/update_item" {
		t.Fatalf("path = %s, want update endpoint", got.Path)
	}
	if got.Body.ItemID == nil || *got.Body.ItemID != 222333444 {
		t.Fatalf("item id = %#v, want existing Shopee item id", got.Body.ItemID)
	}
}

func TestClientApplyReturnsShopeeAPIError(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"error":"error_param","message":"invalid item name"}`))
	}))
	t.Cleanup(server.Close)

	client := mustServerClient(t, server, fixedClock(1777777800))
	_, err := client.Apply(context.Background(), productEvent())
	if err == nil || !strings.Contains(err.Error(), "invalid item name") {
		t.Fatalf("error = %v, want Shopee API error", err)
	}
}

func TestClientApplyRejectsUnsupportedMarketplaceEvent(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{
		BaseURL:     "https://example.invalid",
		PartnerID:   123456,
		PartnerKey:  testPartnerKey,
		AccessToken: "test-access-token",
		ShopID:      987654,
	}, nil)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	event := productEvent()
	event.Operation = marketplacesync.OperationDelete

	_, err = client.Apply(context.Background(), event)
	if err == nil || !strings.Contains(err.Error(), "unsupported operation") {
		t.Fatalf("error = %v, want unsupported operation", err)
	}
}

type productRequest struct {
	Path     string
	RawQuery string
	Body     productPayload
}

func productServer(t *testing.T, got *productRequest, response []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got.Path = r.URL.Path
		got.RawQuery = r.URL.RawQuery
		if err := json.NewDecoder(r.Body).Decode(&got.Body); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		requireShopeeHeaders(t, r)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)
	return server
}

func productEvent() marketplacesync.ProductEvent {
	return marketplacesync.ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopee",
		EntityType: marketplacesync.EntityProduct,
		EntityID:   "sku-300",
		Operation:  marketplacesync.OperationUpsert,
		Version:    "v8-p03-1",
		Payload: map[string]any{
			"title":       "Winter Gloves",
			"description": "Waterproof gloves",
			"sku":         "WG-300",
			"price":       19.95,
			"stock":       42,
		},
	}
}

func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	response, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read response fixture: %v", err)
	}
	return response
}

func mustServerClient(t *testing.T, server *httptest.Server, now func() int64) *Client {
	t.Helper()
	client, err := NewClient(Config{
		BaseURL:     server.URL,
		PartnerID:   123456,
		PartnerKey:  testPartnerKey,
		AccessToken: "test-access-token",
		ShopID:      987654,
		Now:         now,
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func fixedClock(timestamp int64) func() int64 {
	return func() int64 { return timestamp }
}

func requireShopeeHeaders(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", r.Method)
	}
	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
}

func requireSignedRequest(t *testing.T, server *httptest.Server, rawQuery, path string) {
	t.Helper()
	values, err := parseQuery(rawQuery)
	if err != nil {
		t.Fatalf("parse query: %v", err)
	}
	requireQueryValue(t, values, "partner_id", "123456")
	requireQueryValue(t, values, "timestamp", "1777777777")
	requireQueryValue(t, values, "access_token", "test-access-token")
	requireQueryValue(t, values, "shop_id", "987654")
	req := SignRequest{
		PartnerKey:  []byte(testPartnerKey),
		PartnerID:   123456,
		Path:        path,
		Timestamp:   1777777777,
		AccessToken: "test-access-token",
		ShopID:      987654,
	}
	requireQueryValue(t, values, "sign", referenceShopeeSignature(req))
	_ = server
}

func requireQueryValue(t *testing.T, values map[string]string, key, want string) {
	t.Helper()
	if got := values[key]; got != want {
		t.Fatalf("query %s = %q, want %q", key, got, want)
	}
}

func requireProductPayload(t *testing.T, payload productPayload) {
	t.Helper()
	if payload.ItemName != "Winter Gloves" || payload.Description != "Waterproof gloves" {
		t.Fatalf("payload identity = %#v", payload)
	}
	if payload.ItemSKU != "WG-300" {
		t.Fatalf("item sku = %q", payload.ItemSKU)
	}
	if payload.OriginalPrice != 19.95 {
		t.Fatalf("price = %v", payload.OriginalPrice)
	}
	if payload.NormalStock != 42 {
		t.Fatalf("stock = %d", payload.NormalStock)
	}
	if payload.ExternalSKU != "sku-300" {
		t.Fatalf("external sku = %q", payload.ExternalSKU)
	}
}

func requireApplyResult(t *testing.T, result marketplacesync.ApplyResult, remoteID string) {
	t.Helper()
	if result.RemoteID != remoteID {
		t.Fatalf("remote id = %q, want %q", result.RemoteID, remoteID)
	}
	if result.Version != "v8-p03-1" {
		t.Fatalf("version = %q", result.Version)
	}
}

func parseQuery(raw string) (map[string]string, error) {
	parsed, err := url.ParseQuery(raw)
	if err != nil {
		return nil, err
	}
	values := map[string]string{}
	for key, value := range parsed {
		if len(value) > 0 {
			values[key] = value[0]
		}
	}
	return values, nil
}
