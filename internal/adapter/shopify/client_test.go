package shopify

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
)

func TestClientApplyBuildsProductSetRequestFromSyncEvent(t *testing.T) {
	t.Parallel()

	response, err := os.ReadFile(filepath.Join("testdata", "product_set_success.json"))
	if err != nil {
		t.Fatalf("read response fixture: %v", err)
	}

	var got graphQLRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("method = %s, want POST", r.Method)
		}
		if r.URL.Path != "/admin/api/2026-04/graphql.json" {
			t.Fatalf("path = %s, want versioned graphql endpoint", r.URL.Path)
		}
		if got := r.Header.Get("X-Shopify-Access-Token"); got != "token-test" {
			t.Fatalf("access token header = %q, want token-test", got)
		}
		if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
			t.Fatalf("content type = %q, want application/json", got)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{
		BaseURL:     server.URL,
		AccessToken: "token-test",
	}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	result, err := client.Apply(context.Background(), productEvent())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	if result.RemoteID != "gid://shopify/Product/1072481952" {
		t.Fatalf("remote id = %q", result.RemoteID)
	}
	if result.Version != "v8-p02-1" {
		t.Fatalf("version = %q", result.Version)
	}
	if !strings.Contains(got.Query, "productSet") {
		t.Fatalf("query = %q, want productSet mutation", got.Query)
	}

	variables := got.Variables
	if variables["synchronous"] != true {
		t.Fatalf("synchronous = %v, want true", variables["synchronous"])
	}
	identifier := asMap(t, variables["identifier"])
	customID := asMap(t, identifier["customId"])
	if customID["namespace"] != "agentic_ec" || customID["key"] != "entity_id" || customID["value"] != "sku-100" {
		t.Fatalf("custom id = %#v", customID)
	}
	input := asMap(t, variables["input"])
	if input["title"] != "Winter Hat" || input["handle"] != "winter-hat" {
		t.Fatalf("input identity = %#v", input)
	}
	if input["descriptionHtml"] != "Warm merino hat" || input["vendor"] != "Agentic Goods" || input["status"] != "ACTIVE" {
		t.Fatalf("input fields = %#v", input)
	}
	tags, ok := input["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "winter" || tags[1] != "wool" {
		t.Fatalf("tags = %#v", input["tags"])
	}
}

func TestClientApplyUsesExistingShopifyIDWhenExternalIDIsGraphQLID(t *testing.T) {
	t.Parallel()

	var got graphQLRequest
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"data":{"productSet":{"product":{"id":"gid://shopify/Product/222"},"userErrors":[]}}}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BaseURL: server.URL, AccessToken: "token-test"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	event := productEvent()
	event.ExternalID = "gid://shopify/Product/222"

	if _, err := client.Apply(context.Background(), event); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	identifier := asMap(t, got.Variables["identifier"])
	if identifier["id"] != "gid://shopify/Product/222" {
		t.Fatalf("identifier = %#v, want existing product id", identifier)
	}
	if _, ok := identifier["customId"]; ok {
		t.Fatalf("identifier includes customId for existing Shopify id: %#v", identifier)
	}
}

func TestClientApplyRejectsGraphQLUserErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"productSet":{"product":null,"userErrors":[{"field":["input","title"],"message":"Title is required"}]}}}`))
	}))
	t.Cleanup(server.Close)

	client, err := NewClient(Config{BaseURL: server.URL, AccessToken: "token-test"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}

	_, err = client.Apply(context.Background(), productEvent())
	if err == nil || !strings.Contains(err.Error(), "Title is required") {
		t.Fatalf("error = %v, want Shopify user error", err)
	}
}

func TestClientApplyRejectsUnsupportedMarketplaceEvent(t *testing.T) {
	t.Parallel()

	client, err := NewClient(Config{BaseURL: "https://example.invalid", AccessToken: "token-test"}, nil)
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

func productEvent() marketplacesync.ProductEvent {
	return marketplacesync.ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopify",
		EntityType: marketplacesync.EntityProduct,
		EntityID:   "sku-100",
		Operation:  marketplacesync.OperationUpsert,
		Version:    "v8-p02-1",
		Payload: map[string]any{
			"title":            "Winter Hat",
			"handle":           "winter-hat",
			"description_html": "Warm merino hat",
			"product_type":     "Accessories",
			"vendor":           "Agentic Goods",
			"status":           "ACTIVE",
			"tags":             []string{"winter", "wool"},
		},
	}
}

func asMap(t *testing.T, value any) map[string]any {
	t.Helper()
	got, ok := value.(map[string]any)
	if !ok {
		t.Fatalf("value = %#v (%T), want map[string]any", value, value)
	}
	return got
}
