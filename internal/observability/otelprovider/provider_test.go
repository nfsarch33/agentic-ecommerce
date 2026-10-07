package otelprovider

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// The acceptance rows for v18870-2: the exporter being down never reaches
// the caller, and the allow-list is the PII guarantee for a public repo.
func TestExporterDownLeavesAppUnaffected(t *testing.T) {
	// Port 1 on loopback: nothing listens there.
	sd, err := Setup(context.Background(), "http://127.0.0.1:1", "test")
	if err != nil {
		t.Fatalf("Setup must not fail on a down backend: %v", err)
	}
	tr := Tracer("test")
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, span := tr.Start(context.Background(), "approve")
		span.SetAttributes(attribute.String("run.id", "r-1"))
		span.End()
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("span creation blocked on a down exporter")
	}
	shutdownDone := make(chan error, 1)
	go func() { shutdownDone <- sd(context.Background()) }()
	select {
	case <-shutdownDone:
	case <-time.After(20 * time.Second):
		t.Fatal("shutdown blocked on a down exporter")
	}
}

func TestAllowListDropsEverythingNotListed(t *testing.T) {
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(&AllowListProcessor{next: rec}),
	)
	tr := tp.Tracer("test")
	_, span := tr.Start(context.Background(), "woo.put")
	span.SetAttributes(
		attribute.String("http.method", "PUT"),
		attribute.String("http.route", "/wp-json/wc/v3/products"),
		attribute.Int("http.status_code", 200),
		attribute.String("run.id", "run-42"),
		// The canaries: value shapes a public repo must never export.
		attribute.String("http.url", "https://store.example.com/wp-json/wc/v3/products"),
		attribute.String("prompt", "draft a warm product description for the merino beanie"),
		attribute.String("customer.email", "someone@example.com"),
		attribute.String("api_key", "ck_xoxb_canary"),
		attribute.String("db.statement", "SELECT * FROM orders WHERE email='someone@example.com'"),
	)
	span.AddEvent("retry", trace.WithAttributes(attribute.String("prompt", "canary in an event")))
	span.End()

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorder saw %d spans, want 1", len(spans))
	}
	dump := ""
	for _, kv := range spans[0].Attributes() {
		dump += string(kv.Key) + "=" + kv.Value.Emit() + "\n"
	}
	for _, canary := range []string{"store.example.com", "merino", "someone@example.com", "ck_xoxb_canary", "SELECT * FROM"} {
		if strings.Contains(dump, canary) {
			t.Fatalf("canary %q leaked into exported attributes:\n%s", canary, dump)
		}
	}
	for _, want := range []string{"http.method=PUT", "run.id=run-42"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("allowed attribute %s missing:\n%s", want, dump)
		}
	}
	evs := spans[0].Events()
	if len(evs) != 1 {
		t.Fatalf("one event expected, saw %d", len(evs))
	}
	if len(evs[0].Attributes) != 0 {
		t.Fatalf("event attributes not filtered: %+v", evs[0].Attributes)
	}
	if err := tp.Shutdown(context.Background()); err != nil {
		t.Fatalf("shutdown: %v", err)
	}
}

func TestSetupEmptyEndpointIsNoop(t *testing.T) {
	sd, err := Setup(context.Background(), "", "test")
	if err != nil {
		t.Fatalf("empty endpoint must not error: %v", err)
	}
	if err := sd(context.Background()); err != nil {
		t.Fatalf("noop shutdown must not error: %v", err)
	}
}
