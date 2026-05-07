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
	if cfg.ScheduleEnabled {
		t.Fatal("ScheduleEnabled = true, want disabled by default")
	}
	if cfg.ScheduleDefaultInterval != 15*time.Minute {
		t.Fatalf("ScheduleDefaultInterval = %s, want 15m", cfg.ScheduleDefaultInterval)
	}
	if cfg.ScheduleMaxConcurrentRuns != 1 {
		t.Fatalf("ScheduleMaxConcurrentRuns = %d, want 1", cfg.ScheduleMaxConcurrentRuns)
	}
	if cfg.ScheduleTaskQueue != "ec-workflows" {
		t.Fatalf("ScheduleTaskQueue = %q, want ec-workflows", cfg.ScheduleTaskQueue)
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

func TestLoadConfigUsesAgentScheduleOverrides(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfig(func(key string) string {
		switch key {
		case "ECOMMERCE_AGENT_SCHEDULES_ENABLED":
			return "true"
		case "ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL":
			return "30m"
		case "ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS":
			return "3"
		case "ECOMMERCE_AGENT_SCHEDULES_TASK_QUEUE":
			return "agent-schedules"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if !cfg.ScheduleEnabled {
		t.Fatal("ScheduleEnabled = false, want true")
	}
	if cfg.ScheduleDefaultInterval != 30*time.Minute {
		t.Fatalf("ScheduleDefaultInterval = %s, want 30m", cfg.ScheduleDefaultInterval)
	}
	if cfg.ScheduleMaxConcurrentRuns != 3 {
		t.Fatalf("ScheduleMaxConcurrentRuns = %d, want 3", cfg.ScheduleMaxConcurrentRuns)
	}
	if cfg.ScheduleTaskQueue != "agent-schedules" {
		t.Fatalf("ScheduleTaskQueue = %q, want agent-schedules", cfg.ScheduleTaskQueue)
	}
}

func TestLoadConfigFallsBackToTemporalTaskQueueForSchedules(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfig(func(key string) string {
		if key == "ECOMMERCE_TEMPORAL_TASK_QUEUE" {
			return "custom-workflows"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if cfg.ScheduleTaskQueue != "custom-workflows" {
		t.Fatalf("ScheduleTaskQueue = %q, want custom-workflows", cfg.ScheduleTaskQueue)
	}
}

func TestRunOnceExecutesDeterministicAgentThroughOrchestrator(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := Config{
		Enabled:                   true,
		RunOnce:                   true,
		Concurrency:               2,
		Interval:                  time.Minute,
		ScheduleEnabled:           true,
		ScheduleDefaultInterval:   time.Minute,
		ScheduleMaxConcurrentRuns: 2,
		ScheduleTaskQueue:         "ec-workflows",
		MetricsAddr:               "127.0.0.1:0",
		EventBusDriver:            "redis",
		SyncChannel:               "ec.sync.events",
		DLQChannel:                "ec.sync.deadletter",
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

func TestRunOnceSkipsSchedulerWhenAgentSchedulesDisabled(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := Config{
		Enabled:                   true,
		RunOnce:                   true,
		Concurrency:               2,
		Interval:                  time.Minute,
		ScheduleEnabled:           false,
		ScheduleDefaultInterval:   time.Minute,
		ScheduleMaxConcurrentRuns: 2,
		ScheduleTaskQueue:         "ec-workflows",
		MetricsAddr:               "127.0.0.1:0",
		EventBusDriver:            "redis",
		SyncChannel:               "ec.sync.events",
		DLQChannel:                "ec.sync.deadletter",
	}

	if err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), cfg); err != nil {
		t.Fatalf("run returned error: %v", err)
	}

	logs := buf.String()
	if !strings.Contains(logs, "agent-worker.schedules_disabled") {
		t.Fatalf("logs missing schedules disabled event:\n%s", logs)
	}
	if strings.Contains(logs, "agent-worker.scheduler_run_succeeded") {
		t.Fatalf("disabled schedules still ran jobs:\n%s", logs)
	}
}

func TestRunDisabledSkipsScheduler(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	cfg := Config{Enabled: false, MetricsAddr: "127.0.0.1:0", Interval: time.Minute, Concurrency: 1}

	if err := run(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)), cfg); err != nil {
		t.Fatalf("run returned error: %v", err)
	}
	if !strings.Contains(buf.String(), "agent-worker.disabled") {
		t.Fatalf("logs missing disabled event:\n%s", buf.String())
	}
}

func TestRunStartsMetricsServerAndShutsDownOnContextCancel(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)
	cfg := Config{
		Enabled:                   true,
		Concurrency:               1,
		Interval:                  time.Hour,
		ScheduleEnabled:           false,
		ScheduleDefaultInterval:   time.Hour,
		ScheduleMaxConcurrentRuns: 1,
		ScheduleTaskQueue:         "ec-workflows",
		MetricsAddr:               "127.0.0.1:0",
		EventBusDriver:            "redis",
		SyncChannel:               "ec.sync.events",
		DLQChannel:                "ec.sync.deadletter",
	}

	go func() {
		errCh <- run(ctx, slog.New(slog.NewJSONHandler(&buf, nil)), cfg)
	}()
	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("run returned error: %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("run did not shut down after context cancellation")
	}
	if logs := buf.String(); !strings.Contains(logs, "agent-worker.start") || !strings.Contains(logs, "agent-worker.shutdown") {
		t.Fatalf("logs missing lifecycle events:\n%s", logs)
	}
}

func TestMetricsHandlerExposesAgentWorkerMetrics(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Enabled:                   true,
		Concurrency:               3,
		Interval:                  30 * time.Second,
		ScheduleEnabled:           true,
		ScheduleDefaultInterval:   15 * time.Minute,
		ScheduleMaxConcurrentRuns: 2,
		ScheduleTaskQueue:         "ec-workflows",
		MetricsAddr:               "127.0.0.1:0",
		EventBusDriver:            "redis",
		SyncChannel:               "ec.sync.events",
		DLQChannel:                "ec.sync.deadletter",
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
		"agentic_ecommerce_agent_schedules_enabled",
		"agentic_ecommerce_agent_schedule_default_interval_seconds",
		"agentic_ecommerce_agent_schedule_max_concurrent_runs",
		"agentic_ecommerce_agent_schedule_config_info",
		"agentic_ecommerce_agent_scheduled_runs_total",
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

func TestParseHelpersAcceptDocumentedValuesAndRejectInvalidInput(t *testing.T) {
	t.Parallel()

	boolTests := []struct {
		raw      string
		fallback bool
		want     bool
	}{
		{raw: "", fallback: true, want: true},
		{raw: "YES", want: true},
		{raw: "off", fallback: true, want: false},
	}
	for _, tt := range boolTests {
		got, err := parseBool(tt.raw, tt.fallback)
		if err != nil {
			t.Fatalf("parseBool(%q): %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseBool(%q) = %v, want %v", tt.raw, got, tt.want)
		}
	}
	if _, err := parseBool("sometimes", false); err == nil {
		t.Fatal("parseBool accepted invalid input")
	}

	if got, err := parsePositiveInt(" 7 ", 1); err != nil || got != 7 {
		t.Fatalf("parsePositiveInt = %d, %v; want 7 nil", got, err)
	}
	if _, err := parsePositiveInt("0", 1); err == nil {
		t.Fatal("parsePositiveInt accepted zero")
	}

	durationTests := []struct {
		raw  string
		want time.Duration
	}{
		{raw: "", want: 5 * time.Second},
		{raw: "250ms", want: 250 * time.Millisecond},
		{raw: "15", want: 15 * time.Second},
	}
	for _, tt := range durationTests {
		got, err := parseDuration(tt.raw, 5*time.Second)
		if err != nil {
			t.Fatalf("parseDuration(%q): %v", tt.raw, err)
		}
		if got != tt.want {
			t.Fatalf("parseDuration(%q) = %s, want %s", tt.raw, got, tt.want)
		}
	}
	for _, raw := range []string{"0", "-1s", "forever"} {
		if _, err := parseDuration(raw, time.Second); err == nil {
			t.Fatalf("parseDuration(%q) accepted invalid input", raw)
		}
	}
}
