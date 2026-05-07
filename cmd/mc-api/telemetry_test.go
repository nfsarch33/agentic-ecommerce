package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestTraceIDFromTraceparent(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{
			name:   "valid",
			header: "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01",
			want:   "4bf92f3577b34da6a3ce929d0e0e4736",
		},
		{
			name:   "uppercase trace id",
			header: "00-4BF92F3577B34DA6A3CE929D0E0E4736-00f067aa0ba902b7-01",
			want:   "4bf92f3577b34da6a3ce929d0e0e4736",
		},
		{name: "wrong version", header: "ff-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{name: "zero trace", header: "00-00000000000000000000000000000000-00f067aa0ba902b7-01"},
		{name: "bad hex", header: "00-zzzz2f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"},
		{name: "missing parts", header: "00-4bf92f3577b34da6a3ce929d0e0e4736"},
	}
	for _, tt := range tests {
		if got := traceIDFromTraceparent(tt.header); got != tt.want {
			t.Fatalf("%s trace ID = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestTraceIDFromRequestUsesTraceparentWhenNoSpan(t *testing.T) {
	t.Parallel()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	req.Header.Set("Traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	if got := traceIDFromRequest(req); got != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("traceIDFromRequest = %q", got)
	}
	if got := traceIDFromRequest(nil); got != "" {
		t.Fatalf("nil traceIDFromRequest = %q, want empty", got)
	}
}
