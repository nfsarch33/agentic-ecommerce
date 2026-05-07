package main

import (
	"bytes"
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestLoadConfigUsesSchedulerDefaults(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if !cfg.Enabled {
		t.Fatal("Enabled = false, want true")
	}
	if cfg.Concurrency != 1 {
		t.Fatalf("Concurrency = %d, want 1", cfg.Concurrency)
	}
	if cfg.Interval != 5*time.Minute {
		t.Fatalf("Interval = %s, want 5m", cfg.Interval)
	}
	if cfg.MetricsAddr != "127.0.0.1:8081" {
		t.Fatalf("MetricsAddr = %q, want loopback default", cfg.MetricsAddr)
	}
}

func TestLoadConfigRejectsInvalidSchedulerValues(t *testing.T) {
	t.Parallel()

	_, err := loadConfig(func(key string) string {
		switch key {
		case "ECOMMERCE_AGENT_WORKER_CONCURRENCY":
			return "0"
		default:
			return ""
		}
	})
	if err == nil {
		t.Fatal("loadConfig returned nil error for invalid concurrency")
	}
}

func TestRunOnceExecutesDeterministicAgentThroughOrchestrator(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := Config{
		Enabled:        true,
		RunOnce:        true,
		Concurrency:    2,
		Interval:       time.Minute,
		MetricsAddr:    "127.0.0.1:0",
		EventBusDriver: "redis",
		SyncChannel:    "ec.sync.events",
		DLQChannel:     "ec.sync.deadletter",
	}

	if err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), cfg); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	logs := buf.String()
	for _, want := range []string{"agent-worker.run_once", "agent-worker.scheduler_run_succeeded", `"agent_id":"compliance"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs missing %q:\n%s", want, logs)
		}
	}
	if strings.Contains(logs, "agent-worker.scheduler_placeholder") {
		t.Fatalf("logs still contain placeholder scheduler hook:\n%s", logs)
	}
}

func TestMetricsHandlerExposesAgentWorkerMetrics(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Enabled:        true,
		Concurrency:    3,
		Interval:       30 * time.Second,
		MetricsAddr:    "127.0.0.1:0",
		EventBusDriver: "redis",
		SyncChannel:    "ec.sync.events",
		DLQChannel:     "ec.sync.deadletter",
	}
	handler := metricsHandler(cfg)
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	handler(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	for _, want := range []string{
		"agentic_ecommerce_agent_worker_build_info",
		"agentic_ecommerce_agent_worker_enabled",
		"agentic_ecommerce_agent_worker_concurrency",
		"agentic_ecommerce_agent_worker_scheduler_interval_seconds",
		"agentic_ecommerce_agent_worker_runs_total",
		`agentic_ecommerce_agent_worker_runs_total{eventbus_driver="redis",sync_channel="ec.sync.events",status="succeeded"}`,
		`agentic_ecommerce_agent_worker_runs_total{eventbus_driver="redis",sync_channel="ec.sync.events",status="failed"}`,
		"agentic_ecommerce_agent_worker_compliance_checks_total",
		"agentic_ecommerce_agent_worker_compliance_failures_total",
		"agentic_ecommerce_agent_worker_media_validation_failures_total",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
	if strings.Contains(body, "agentic_ecommerce_agent_worker_placeholder_runs_total") {
		t.Fatalf("metrics still expose placeholder counter:\n%s", body)
	}
}

func TestWorkerMuxExposesHealthzAndRejectsInvalidMethods(t *testing.T) {
	t.Parallel()

	handler := workerMux(Config{Enabled: true, Concurrency: 1, Interval: time.Minute})
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("healthz status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "agentic-ecommerce-agent-worker") {
		t.Fatalf("healthz body = %s", rec.Body.String())
	}

	req = httptest.NewRequest(http.MethodPost, "/healthz", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("post healthz status = %d, want 405", rec.Code)
	}
}

func TestRunHealthcheckUsesLoopbackForWildcardAddress(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: workerMux(Config{Enabled: true, Concurrency: 1, Interval: time.Minute})}
	t.Cleanup(func() {
		_ = server.Close()
	})
	go func() {
		_ = server.Serve(listener)
	}()

	_, port, err := net.SplitHostPort(listener.Addr().String())
	if err != nil {
		t.Fatalf("split host port: %v", err)
	}
	if err := runHealthcheck("0.0.0.0:" + port); err != nil {
		t.Fatalf("runHealthcheck: %v", err)
	}
}

func TestHealthcheckArgs(t *testing.T) {
	t.Parallel()

	for _, args := range [][]string{{"agent-worker", "healthcheck"}, {"agent-worker", "--healthcheck"}} {
		if !isHealthcheckArgs(args) {
			t.Fatalf("isHealthcheckArgs(%v) = false, want true", args)
		}
	}
	if isHealthcheckArgs([]string{"agent-worker"}) {
		t.Fatal("isHealthcheckArgs without healthcheck arg = true, want false")
	}
}
