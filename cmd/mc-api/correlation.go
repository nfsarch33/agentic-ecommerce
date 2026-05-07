package main

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type requestCorrelationContextKey struct{}

type requestCorrelation struct {
	RequestID string
	TenantID  string
	ActorID   string
}

func requestCorrelationFromContext(ctx context.Context) *requestCorrelation {
	corr, _ := ctx.Value(requestCorrelationContextKey{}).(*requestCorrelation)
	return corr
}

func routePattern(r *http.Request) string {
	if r == nil {
		return ""
	}
	if r.Pattern != "" {
		return r.Pattern
	}
	path := r.URL.Path
	switch {
	case path == "/healthz", path == "/readyz", path == "/metrics":
		return path
	case path == "/api/v1/products" || strings.HasPrefix(path, "/api/v1/products/"):
		return "/api/v1/products"
	case path == "/api/v1/orders" || strings.HasPrefix(path, "/api/v1/orders/"):
		return "/api/v1/orders"
	case strings.HasPrefix(path, "/api/v1/cart/"):
		return "/api/v1/cart/{session_id}"
	case path == "/api/v1/agents" || strings.HasPrefix(path, "/api/v1/agents/"):
		return "/api/v1/agents"
	case path == "/api/v1/workflows" || strings.HasPrefix(path, "/api/v1/workflows/"):
		return "/api/v1/workflows"
	default:
		return path
	}
}

func clientIPFromRequest(r *http.Request) string {
	if r == nil {
		return ""
	}
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	if realIP := strings.TrimSpace(r.Header.Get("X-Real-IP")); realIP != "" {
		return realIP
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil && host != "" {
		return host
	}
	return strings.TrimSpace(r.RemoteAddr)
}
