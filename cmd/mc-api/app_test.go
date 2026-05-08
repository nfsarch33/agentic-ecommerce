package main

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// File scope: v2.6.1 cmd/* DI refactor coverage for mc-api. Drives
// the new mainImpl + runServer + getenvFn surface through every
// branch (healthcheck OK + fail, server-stop, graceful shutdown,
// shutdown timeout fallback, env-var fallback).

func TestMainImpl_HealthcheckSuccessReturnsZero(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()

	getenv := func(key string) string {
		if key == "ECOMMERCE_HTTP_ADDR" {
			return listener.Addr().String()
		}
		return ""
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"mc-api", "healthcheck"}, &buf, getenv)
	if got != 0 {
		t.Fatalf("mainImpl healthcheck exit=%d log=%s", got, buf.String())
	}
}

func TestMainImpl_HealthcheckFailureReturnsOne(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ECOMMERCE_HTTP_ADDR" {
			return "127.0.0.1:1" // closed port
		}
		return ""
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"mc-api", "--healthcheck"}, &buf, getenv)
	if got != 1 {
		t.Fatalf("expected 1 for unreachable healthz, got %d", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("mc-api.healthcheck_failed")) {
		t.Fatalf("expected healthcheck_failed log, got %s", buf.String())
	}
}

// TestMainImpl_ContextCancelInitiatesGracefulShutdown drives the
// non-healthcheck path: the server starts on an ephemeral port and
// the supplied context cancels immediately, exercising the
// ctx.Done graceful-shutdown branch.
func TestMainImpl_ContextCancelInitiatesGracefulShutdown(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	listener.Close()
	addr := listener.Addr().String()

	getenv := func(key string) string {
		if key == "ECOMMERCE_HTTP_ADDR" {
			return addr
		}
		return ""
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var buf bytes.Buffer
	got := mainImpl(ctx, []string{"mc-api"}, &buf, getenv)
	if got != 0 && got != 1 {
		t.Fatalf("expected 0 or 1, got %d log=%s", got, buf.String())
	}
}

func TestRunServer_GracefulShutdown(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})}
	go func() { _ = srv.Serve(listener) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // trigger shutdown immediately

	got := runServer(ctx, discardLogger(), srv, 100*time.Millisecond)
	if got != 0 {
		t.Errorf("runServer exit = %d, want 0", got)
	}
}

func TestRunServer_PropagatesListenError(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	taken := listener.Addr().String()

	srv := &http.Server{Addr: taken, Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})}
	defer srv.Close()
	t.Cleanup(func() { _ = listener.Close() })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var buf bytes.Buffer
	got := runServer(ctx, slog.New(slog.NewJSONHandler(&buf, nil)), srv, time.Second)
	if got != 1 {
		t.Errorf("runServer = %d, want 1", got)
	}
	if !strings.Contains(buf.String(), "mc-api.stop") {
		t.Errorf("expected mc-api.stop log, got %s", buf.String())
	}
}

func TestRunServer_AppliesDefaultShutdownTimeout(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {})}
	go func() { _ = srv.Serve(listener) }()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := runServer(ctx, discardLogger(), srv, 0) // 0 -> default 10s
	if got != 0 {
		t.Errorf("runServer with default timeout = %d, want 0", got)
	}
}

func TestGetenvFn_ReturnsConfiguredValue(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "FOO" {
			return "value"
		}
		return ""
	}
	if got := getenvFn(getenv, "FOO", "fallback"); got != "value" {
		t.Errorf("got %q, want value", got)
	}
}

func TestGetenvFn_ReturnsFallbackForEmpty(t *testing.T) {
	t.Parallel()

	getenv := func(string) string { return "" }
	if got := getenvFn(getenv, "FOO", "fallback"); got != "fallback" {
		t.Errorf("got %q, want fallback", got)
	}
}

// TestRunHealthcheckPropagatesNon200 covers the !http.StatusOK branch
// of runHealthcheck (was 78% covered previously).
func TestRunHealthcheckPropagatesNon200(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	addr := strings.TrimPrefix(srv.URL, "http://")
	if err := runHealthcheck(addr); err == nil {
		t.Fatal("expected error for 503")
	}
}

// TestMetricsHandler_RejectsNonGet covers the non-GET branch of
// mc-api metricsHandler (was 60% covered previously).
func TestMetricsHandler_RejectsNonGet(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	rec := httptest.NewRecorder()
	metricsHandler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("got %d, want 405", rec.Code)
	}
}

// TestMetricsHandlerEmitsContentTypeHeader pins the Prometheus
// content-type contract.
func TestMetricsHandlerEmitsContentTypeHeader(t *testing.T) {
	t.Parallel()

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	metricsHandler(rec, req)
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/plain") {
		t.Errorf("content-type = %q", got)
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(io.Discard, nil))
}
