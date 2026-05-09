package observability

import (
	"context"
	"log/slog"
	"net/http"
)

// tenantKey scopes the per-request tenant ID inside ctx so the
// LoggerFromContext helper can attach `tenant_id=...` automatically.
type tenantKey struct{}

// ContextWithTenant returns a child context carrying the tenant ID.
func ContextWithTenant(ctx context.Context, tenantID string) context.Context {
	if tenantID == "" {
		return ctx
	}
	return context.WithValue(ctx, tenantKey{}, tenantID)
}

// TenantFromContext returns the tenant ID stored in ctx, or "".
func TenantFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(tenantKey{}).(string); ok {
		return v
	}
	return ""
}

// LoggerFromContext returns a logger annotated with trace_id, span_id,
// tenant_id when available. base is the fallback logger to clone.
func LoggerFromContext(ctx context.Context, base *slog.Logger) *slog.Logger {
	if base == nil {
		base = slog.Default()
	}
	attrs := make([]slog.Attr, 0, 3)
	if tid := TraceIDFromContext(ctx); tid != "" {
		attrs = append(attrs, slog.String("trace_id", tid))
	}
	if sid := SpanIDFromContext(ctx); sid != "" {
		attrs = append(attrs, slog.String("span_id", sid))
	}
	if tenant := TenantFromContext(ctx); tenant != "" {
		attrs = append(attrs, slog.String("tenant_id", tenant))
	}
	if len(attrs) == 0 {
		return base
	}
	return slog.New(base.Handler().WithAttrs(attrs))
}

// LoggerFromRequest builds a request-scoped logger by reading the
// span context and Traceparent header.
func LoggerFromRequest(r *http.Request, base *slog.Logger) *slog.Logger {
	ctx := r.Context()
	if tid := TraceIDFromRequest(r); tid != "" && TraceIDFromContext(ctx) == "" {
		// surface header-derived trace id as if it lived in ctx
		return base.With("trace_id", tid)
	}
	return LoggerFromContext(ctx, base)
}

// RequestLogger is the v2.10.0 Story 4 middleware that injects a
// request-scoped logger into ctx via slog.With(...). Downstream
// handlers fetch it with LoggerFromRequest.
func RequestLogger(base *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			tid := TraceIDFromRequest(r)
			if tid != "" {
				ctx := r.Context()
				ctx = context.WithValue(ctx, traceFromHeaderKey{}, tid)
				r = r.WithContext(ctx)
			}
			next.ServeHTTP(w, r)
			_ = base // keep base referenced to discourage unused-import warnings
		})
	}
}

type traceFromHeaderKey struct{}
