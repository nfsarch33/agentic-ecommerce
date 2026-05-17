package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
)

func newAPISeparationQAServer(t *testing.T) *server {
	t.Helper()

	srv := newServer(
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		inmemory.NewProductRepository(),
		inmemory.NewOrderRepository(),
		inmemory.NewCartRepository(),
	)
	srv.rateLimiter = nil
	t.Cleanup(srv.Close)
	return srv
}

func requestAgainstServer(t *testing.T, srv *server, method, path string) *http.Response {
	t.Helper()

	httpSrv := httptest.NewServer(srv.mux())
	t.Cleanup(httpSrv.Close)

	client := &http.Client{Timeout: 500 * time.Millisecond}
	req, err := http.NewRequest(method, httpSrv.URL+path, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, path, err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestAgentActivityStreamRouteIsMountedAtPublicMux(t *testing.T) {
	t.Parallel()

	resp := requestAgainstServer(t, newAPISeparationQAServer(t), http.MethodGet, "/api/v1/agent-activity/stream?tenant_id=tenant-A")

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	if got := resp.Header.Get("Content-Type"); !strings.Contains(got, "text/event-stream") {
		t.Fatalf("content-type = %q, want text/event-stream", got)
	}
}

func TestAgentActivityStreamRejectsMissingTenantAtPublicMux(t *testing.T) {
	t.Parallel()

	resp := requestAgainstServer(t, newAPISeparationQAServer(t), http.MethodGet, "/api/v1/agent-activity/stream")

	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusBadRequest)
	}
}

func TestAgentActivityStreamRejectsPostAtPublicMux(t *testing.T) {
	t.Parallel()

	resp := requestAgainstServer(t, newAPISeparationQAServer(t), http.MethodPost, "/api/v1/agent-activity/stream?tenant_id=tenant-A")

	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusMethodNotAllowed)
	}
}

func TestOperatorAlertOpenAPIDocumentsTenantHeaderFallback(t *testing.T) {
	t.Parallel()

	spec := loadOpenAPISpec(t)
	paths := specMap(t, spec, "paths")

	for _, tc := range []struct {
		path   string
		method string
	}{
		{path: "/api/v1/operator/alerts", method: "get"},
		{path: "/api/v1/operator/alerts/{alert_id}/acknowledge", method: "post"},
		{path: "/api/v1/operator/alerts/{alert_id}/resolve", method: "post"},
	} {
		op := specMap(t, specMap(t, paths, tc.path), tc.method)
		description := tenantIDParameterDescription(t, op)
		if !strings.Contains(strings.ToLower(description), "header") || !strings.Contains(strings.ToLower(description), "fallback") {
			t.Fatalf("%s %s tenant_id description = %q, want header precedence + fallback note", tc.method, tc.path, description)
		}
	}
}

func tenantIDParameterDescription(t *testing.T, operation map[string]any) string {
	t.Helper()

	rawParams, ok := operation["parameters"].([]any)
	if !ok {
		t.Fatalf("operation missing parameters: %#v", operation)
	}
	for _, rawParam := range rawParams {
		param, ok := rawParam.(map[string]any)
		if !ok {
			t.Fatalf("parameter is not an object: %#v", rawParam)
		}
		name, _ := param["name"].(string)
		in, _ := param["in"].(string)
		if name == "tenant_id" && in == "query" {
			description, _ := param["description"].(string)
			return description
		}
	}
	t.Fatalf("tenant_id query parameter missing from operation: %#v", operation)
	return ""
}
