package main

import (
	"bytes"
	"context"
	"log/slog"
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

func TestRunOnceLogsPlaceholderScheduler(t *testing.T) {
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
	for _, want := range []string{"agent-worker.run_once", "agent-worker.scheduler_placeholder"} {
		if !strings.Contains(logs, want) {
			t.Fatalf("logs missing %q:\n%s", want, logs)
		}
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
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("metrics body missing %q:\n%s", want, body)
		}
	}
}
