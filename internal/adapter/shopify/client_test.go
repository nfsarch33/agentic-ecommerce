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

	"github.com/nfsarch33/helixon-ec/internal/marketplacesync"
)

func TestClientApplyBuildsProductSetRequestFromSyncEvent(t *testing.T) {
	t.Parallel()

	var got graphQLRequest
	server := productSetServer(t, &got, mustFixture(t, "product_set_success.json"))
	client := mustServerClient(t, server)

	result, err := client.Apply(context.Background(), productEvent())
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}

	requireApplyResult(t, result, "gid://shopify/Product/1072481952")
	requireProductSetRequest(t, got)
}

func TestClientApplyUsesExistingShopifyIDWhenExternalIDIsGraphQLID(t *testing.T) {
	t.Parallel()

	var got graphQLRequest
	server := productSetServer(t, &got, []byte(`{"data":{"productSet":{"product":{"id":"gid://shopify/Product/222"},"userErrors":[]}}}`))
	client := mustServerClient(t, server)
	event := productEvent()
	event.ExternalID = "gid://shopify/Product/222"

	if _, err := client.Apply(context.Background(), event); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	requireExistingProductIdentifier(t, got, "gid://shopify/Product/222")
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

func mustFixture(t *testing.T, name string) []byte {
	t.Helper()
	response, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatalf("read response fixture: %v", err)
	}
	return response
}

func productSetServer(t *testing.T, got *graphQLRequest, response []byte) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requireShopifyRequest(t, r)
		decodeGraphQLRequest(t, r, got)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(response)
	}))
	t.Cleanup(server.Close)
	return server
}

func requireShopifyRequest(t *testing.T, r *http.Request) {
	t.Helper()
	if r.Method != http.MethodPost {
		t.Fatalf("method = %s, want POST", r.Method)
	}
	requireRequestPath(t, r.URL.Path)
	requireRequestHeader(t, r, "X-Shopify-Access-Token", "token-test")
	requireContentType(t, r)
}

func requireRequestPath(t *testing.T, got string) {
	t.Helper()
	if got != "/admin/api/2026-04/graphql.json" {
		t.Fatalf("path = %s, want versioned graphql endpoint", got)
	}
}

func requireRequestHeader(t *testing.T, r *http.Request, key, want string) {
	t.Helper()
	if got := r.Header.Get(key); got != want {
		t.Fatalf("%s = %q, want %q", key, got, want)
	}
}

func requireContentType(t *testing.T, r *http.Request) {
	t.Helper()
	if got := r.Header.Get("Content-Type"); !strings.HasPrefix(got, "application/json") {
		t.Fatalf("content type = %q, want application/json", got)
	}
}

func decodeGraphQLRequest(t *testing.T, r *http.Request, got *graphQLRequest) {
	t.Helper()
	if err := json.NewDecoder(r.Body).Decode(got); err != nil {
		t.Fatalf("decode request: %v", err)
	}
}

func mustServerClient(t *testing.T, server *httptest.Server) *Client {
	t.Helper()
	client, err := NewClient(Config{BaseURL: server.URL, AccessToken: "token-test"}, server.Client())
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	return client
}

func requireApplyResult(t *testing.T, result marketplacesync.ApplyResult, remoteID string) {
	t.Helper()
	if result.RemoteID != remoteID {
		t.Fatalf("remote id = %q, want %q", result.RemoteID, remoteID)
	}
	if result.Version != "v8-p02-1" {
		t.Fatalf("version = %q", result.Version)
	}
}

func requireProductSetRequest(t *testing.T, request graphQLRequest) {
	t.Helper()
	requireProductSetMutation(t, request.Query)
	requireSynchronous(t, request.Variables)
	requireCustomID(t, request.Variables)
	requireProductInput(t, request.Variables)
}

func requireProductSetMutation(t *testing.T, query string) {
	t.Helper()
	if !strings.Contains(query, "productSet") {
		t.Fatalf("query = %q, want productSet mutation", query)
	}
}

func requireSynchronous(t *testing.T, variables map[string]any) {
	t.Helper()
	if variables["synchronous"] != true {
		t.Fatalf("synchronous = %v, want true", variables["synchronous"])
	}
}

func requireCustomID(t *testing.T, variables map[string]any) {
	t.Helper()
	identifier := asMap(t, variables["identifier"])
	customID := asMap(t, identifier["customId"])
	if customID["namespace"] != "agentic_ec" || customID["key"] != "entity_id" || customID["value"] != "sku-100" {
		t.Fatalf("custom id = %#v", customID)
	}
}

func requireProductInput(t *testing.T, variables map[string]any) {
	t.Helper()
	input := asMap(t, variables["input"])
	requireInputIdentity(t, input)
	requireInputFields(t, input)
	requireInputTags(t, input)
}

func requireInputIdentity(t *testing.T, input map[string]any) {
	t.Helper()
	if input["title"] != "Winter Hat" || input["handle"] != "winter-hat" {
		t.Fatalf("input identity = %#v", input)
	}
}

func requireInputFields(t *testing.T, input map[string]any) {
	t.Helper()
	if input["descriptionHtml"] != "Warm merino hat" || input["vendor"] != "Agentic Goods" || input["status"] != "ACTIVE" {
		t.Fatalf("input fields = %#v", input)
	}
}

func requireInputTags(t *testing.T, input map[string]any) {
	t.Helper()
	tags, ok := input["tags"].([]any)
	if !ok || len(tags) != 2 || tags[0] != "winter" || tags[1] != "wool" {
		t.Fatalf("tags = %#v", input["tags"])
	}
}

func requireExistingProductIdentifier(t *testing.T, request graphQLRequest, id string) {
	t.Helper()
	identifier := asMap(t, request.Variables["identifier"])
	if identifier["id"] != id {
		t.Fatalf("identifier = %#v, want existing product id", identifier)
	}
	if _, ok := identifier["customId"]; ok {
		t.Fatalf("identifier includes customId for existing Shopify id: %#v", identifier)
	}
}
