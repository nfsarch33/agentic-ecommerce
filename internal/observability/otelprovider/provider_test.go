package otelprovider

import (
	"context"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/codes"
	"net/http"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

// The acceptance rows for : the exporter being down never reaches
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

// The security row from the round-1 review: a transport error against a
// URL whose query carries store credentials must NOT export them — the
// span STATUS description (where otelhttp puts the url.Error string) is
// replaced, and link attributes ride the allow-list.
func TestTransportErrorCarriesNoCredentialsInStatus(t *testing.T) {
	sd, err := Setup(context.Background(), "http://127.0.0.1:1", "test")
	if err != nil {
		t.Fatalf("Setup: %v", err)
	}
	defer func() { _ = sd(context.Background()) }()

	// A client span over an unreachable URL with a canary credential in
	// the query — the approve-to-publish PUT shape on a dial failure.
	const canary = "ck_secret_consumer_key_canary_12345"
	u := "http://127.0.0.1:1/v1/products?consumer_key=" + canary + "&consumer_secret=cs_canary"
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, u, nil)
	tr := Tracer("test")
	ctx, span := tr.Start(context.Background(), "woo.put", trace.WithSpanKind(trace.SpanKindClient))
	otelhttp.NewTransport(http.DefaultTransport).RoundTrip(req.WithContext(ctx))
	span.SetStatus(codes.Error, "transport error") // what otelhttp does on failure
	span.AddLink(trace.LinkFromContext(context.Background(),
		attribute.String("url", u), attribute.String("http.method", "GET")))
	span.End()

	// Read the span back through a recorder with the SAME allow-list
	// wrapper the provider installs, and inspect what would export.
	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(&AllowListProcessor{next: rec}))
	_, probe := tp.Tracer("p").Start(context.Background(), "probe")
	probe.SetStatus(codes.Error, "Get \x22"+u+"\x22: dial tcp: connection refused")
	probe.AddLink(trace.LinkFromContext(context.Background(), attribute.String("url", u)))
	probe.End()
	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorder saw %d spans", len(spans))
	}
	dump := spans[0].Status().Description + " "
	for _, kv := range spans[0].Links()[0].Attributes {
		dump += kv.Value.Emit() + " "
	}
	for _, canaryTok := range []string{canary, "cs_canary", "127.0.0.1:1/v1"} {
		if strings.Contains(dump, canaryTok) {
			t.Fatalf("canary %q leaked through status/links:\n%s", canaryTok, dump)
		}
	}
	if spans[0].Status().Code != codes.Error {
		t.Fatal("the error CODE must survive the description strip")
	}
}
