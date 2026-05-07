package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequestCorrelationFromContext(t *testing.T) {
	t.Parallel()
	if got := requestCorrelationFromContext(context.Background()); got != nil {
		t.Fatalf("empty context correlation = %+v, want nil", got)
	}
	corr := &requestCorrelation{RequestID: "req-1", TenantID: "tenant-a", ActorID: "actor-a"}
	ctx := context.WithValue(context.Background(), requestCorrelationContextKey{}, corr)
	if got := requestCorrelationFromContext(ctx); got != corr {
		t.Fatalf("correlation = %+v, want original pointer", got)
	}
}

func TestRoutePatternFallsBackToStableRoutes(t *testing.T) {
	t.Parallel()
	tests := []struct {
		path string
		want string
	}{
		{path: "/healthz", want: "/healthz"},
		{path: "/api/v1/products/123/generate-description", want: "/api/v1/products"},
		{path: "/api/v1/orders/123/status", want: "/api/v1/orders"},
		{path: "/api/v1/cart/session-1", want: "/api/v1/cart/{session_id}"},
		{path: "/api/v1/agents/sourcing/run", want: "/api/v1/agents"},
		{path: "/api/v1/workflows/run-1", want: "/api/v1/workflows"},
		{path: "/unknown", want: "/unknown"},
	}
	for _, tt := range tests {
		req := httptest.NewRequest(http.MethodGet, tt.path, nil)
		if got := routePattern(req); got != tt.want {
			t.Fatalf("routePattern(%q) = %q, want %q", tt.path, got, tt.want)
		}
	}
	if got := routePattern(nil); got != "" {
		t.Fatalf("nil routePattern = %q, want empty", got)
	}
}

func TestRoutePatternPrefersServeMuxPattern(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/products/123", nil)
	req.Pattern = "/api/v1/products/"
	if got := routePattern(req); got != "/api/v1/products/" {
		t.Fatalf("routePattern = %q, want ServeMux pattern", got)
	}
}

func TestClientIPFromRequest(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.RemoteAddr = "10.0.0.10:54321"
	if got := clientIPFromRequest(req); got != "10.0.0.10" {
		t.Fatalf("client IP from remote addr = %q", got)
	}
	req.Header.Set("X-Real-IP", "10.0.0.20")
	if got := clientIPFromRequest(req); got != "10.0.0.20" {
		t.Fatalf("client IP from real IP = %q", got)
	}
	req.Header.Set("X-Forwarded-For", "10.0.0.30, 10.0.0.31")
	if got := clientIPFromRequest(req); got != "10.0.0.30" {
		t.Fatalf("client IP from forwarded = %q", got)
	}
	if got := clientIPFromRequest(nil); got != "" {
		t.Fatalf("nil client IP = %q, want empty", got)
	}
}
