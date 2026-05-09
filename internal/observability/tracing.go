// Package observability ships shared OpenTelemetry helpers used by
// every cmd/* binary so trace context propagation, slog correlation,
// and request-scoped logger injection are implemented in exactly one
// place.
//
// v2.10.0 Story 4: every request observable end-to-end. Logs carry
// trace_id + span_id + tenant_id when present so Grafana dashboards
// can correlate metrics, traces, and logs by clicking through.
package observability

import (
	"context"
	"net/http"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

// TraceIDFromContext returns the active trace ID encoded as a 32-char
// hex string, or "" if the context carries no valid span.
func TraceIDFromContext(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return ""
	}
	return span.TraceID().String()
}

// SpanIDFromContext returns the active span ID encoded as 16-char
// hex string, or "".
func SpanIDFromContext(ctx context.Context) string {
	span := trace.SpanContextFromContext(ctx)
	if !span.IsValid() {
		return ""
	}
	return span.SpanID().String()
}

// TraceIDFromRequest prefers the request context's span; if absent it
// falls back to parsing the Traceparent header. Returns "" if neither
// source is valid.
func TraceIDFromRequest(r *http.Request) string {
	if id := TraceIDFromContext(r.Context()); id != "" {
		return id
	}
	return ParseTraceparent(r.Header.Get("Traceparent"))
}

// ParseTraceparent extracts the trace_id field from a W3C Trace
// Context "traceparent" header. Returns "" for malformed input.
func ParseTraceparent(header string) string {
	parts := strings.Split(strings.TrimSpace(header), "-")
	if len(parts) != 4 || parts[0] != "00" {
		return ""
	}
	traceID := strings.ToLower(parts[1])
	if len(traceID) != 32 || traceID == "00000000000000000000000000000000" {
		return ""
	}
	for _, ch := range traceID {
		if (ch < '0' || ch > '9') && (ch < 'a' || ch > 'f') {
			return ""
		}
	}
	return traceID
}
