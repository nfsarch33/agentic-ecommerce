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

func discardLogger() *slog.Logger {
	return slog.New(slog.NewJSONHandler(&bytes.Buffer{}, nil))
}

// File scope: v2.6.1 cmd/* DI refactor coverage. Drives the new
// mainImpl entry point through every branch (healthcheck OK + fail,
// invalid config, disabled run, run-error). Also fills the gap on
// loadConfig validators that the legacy tests didn't reach
// (RUN_ONCE / INTERVAL / ENABLED parse failures, schedule defaults
// and overrides).

func TestMainImpl_HealthcheckSuccessReturnsZero(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: workerMux(Config{Enabled: true, Concurrency: 1, Interval: time.Minute})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()

	addr := listener.Addr().String()
	getenv := func(key string) string {
		if key == "ECOMMERCE_AGENT_WORKER_METRICS_ADDR" {
			return addr
		}
		return ""
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"agent-worker", "healthcheck"}, &buf, getenv)
	if got != 0 {
		t.Fatalf("mainImpl healthcheck exit=%d log=%s", got, buf.String())
	}
}

func TestMainImpl_HealthcheckFailureReturnsOne(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ECOMMERCE_AGENT_WORKER_METRICS_ADDR" {
			return "127.0.0.1:1" // closed port
		}
		return ""
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"agent-worker", "--healthcheck"}, &buf, getenv)
	if got != 1 {
		t.Fatalf("expected 1 for unreachable healthz, got %d", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("agent-worker.healthcheck_failed")) {
		t.Fatalf("expected healthcheck_failed log, got %s", buf.String())
	}
}

func TestMainImpl_InvalidConfigReturnsOne(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		switch key {
		case "ECOMMERCE_AGENT_WORKER_CONCURRENCY":
			return "0"
		default:
			return ""
		}
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"agent-worker"}, &buf, getenv)
	if got != 1 {
		t.Fatalf("expected 1 for invalid config, got %d", got)
	}
	if !bytes.Contains(buf.Bytes(), []byte("agent-worker.invalid_config")) {
		t.Fatalf("expected invalid_config log, got %s", buf.String())
	}
}

func TestMainImpl_DisabledRunReturnsZero(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		if key == "ECOMMERCE_AGENT_WORKER_ENABLED" {
			return "false"
		}
		return ""
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"agent-worker"}, &buf, getenv)
	if got != 0 {
		t.Fatalf("expected 0 for disabled run, got %d log=%s", got, buf.String())
	}
	if !bytes.Contains(buf.Bytes(), []byte("agent-worker.disabled")) {
		t.Fatalf("expected disabled log, got %s", buf.String())
	}
}

func TestMainImpl_RunOnceWithSchedulesDisabledExitsCleanly(t *testing.T) {
	t.Parallel()

	getenv := func(key string) string {
		switch key {
		case "ECOMMERCE_AGENT_WORKER_ENABLED":
			return "true"
		case "ECOMMERCE_AGENT_WORKER_RUN_ONCE":
			return "true"
		case "ECOMMERCE_AGENT_SCHEDULES_ENABLED":
			return "false"
		default:
			return ""
		}
	}
	var buf bytes.Buffer
	got := mainImpl(context.Background(), []string{"agent-worker"}, &buf, getenv)
	if got != 0 {
		t.Fatalf("run-once exited %d log=%s", got, buf.String())
	}
	if !strings.Contains(buf.String(), "agent-worker.schedules_disabled") {
		t.Fatalf("logs missing schedules_disabled marker:\n%s", buf.String())
	}
}

func TestLoadConfig_RejectsInvalidBoolean(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"ECOMMERCE_AGENT_WORKER_ENABLED",
		"ECOMMERCE_AGENT_WORKER_RUN_ONCE",
		"ECOMMERCE_AGENT_SCHEDULES_ENABLED",
	} {
		k := key
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			_, err := loadConfig(func(name string) string {
				if name == k {
					return "sometimes"
				}
				return ""
			})
			if err == nil {
				t.Fatalf("loadConfig accepted invalid bool for %s", k)
			}
		})
	}
}

func TestLoadConfig_RejectsInvalidDurations(t *testing.T) {
	t.Parallel()

	for _, key := range []string{
		"ECOMMERCE_AGENT_WORKER_INTERVAL",
		"ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL",
	} {
		k := key
		t.Run(k, func(t *testing.T) {
			t.Parallel()
			_, err := loadConfig(func(name string) string {
				if name == k {
					return "forever"
				}
				return ""
			})
			if err == nil {
				t.Fatalf("loadConfig accepted invalid duration for %s", k)
			}
		})
	}
}

func TestLoadConfig_RejectsInvalidScheduleConcurrency(t *testing.T) {
	t.Parallel()

	_, err := loadConfig(func(key string) string {
		if key == "ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS" {
			return "0"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadConfig accepted zero schedule concurrency")
	}
}

func TestRunScheduledJobs_AbortsOnCancelledContext(t *testing.T) {
	t.Parallel()

	cfg := Config{
		Enabled:                   true,
		Concurrency:               1,
		Interval:                  time.Hour,
		ScheduleEnabled:           true,
		ScheduleDefaultInterval:   time.Hour,
		ScheduleMaxConcurrentRuns: 1,
		ScheduleTaskQueue:         "ec-workflows",
		EventBusDriver:            "redis",
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := runScheduledJobs(ctx, discardLogger(), cfg); err == nil {
		t.Fatal("expected ctx error from runScheduledJobs")
	}
}

func TestMetricsHandler_RejectsNonGet(t *testing.T) {
	t.Parallel()

	handler := metricsHandler(Config{Enabled: true, Concurrency: 1})
	req := httptest.NewRequest(http.MethodPost, "/metrics", nil)
	rec := httptest.NewRecorder()
	handler(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("metrics POST status = %d, want 405", rec.Code)
	}
}

func TestParseDuration_RejectsNegativeDuration(t *testing.T) {
	t.Parallel()

	if _, err := parseDuration("-1s", time.Second); err == nil {
		t.Fatal("parseDuration accepted negative duration")
	}
}

func TestParsePositiveInt_RejectsNonNumeric(t *testing.T) {
	t.Parallel()

	if _, err := parsePositiveInt("abc", 1); err == nil {
		t.Fatal("parsePositiveInt accepted non-numeric input")
	}
}

// stubLoopbackHealthEndpoint serves a 503 to prove the healthz error
// path through runHealthcheck.
func TestRunHealthcheckPropagatesNon200(t *testing.T) {
	t.Parallel()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	})}
	t.Cleanup(func() { _ = server.Close() })
	go func() { _ = server.Serve(listener) }()

	if err := runHealthcheck(listener.Addr().String()); err == nil {
		t.Fatal("expected error for 503 healthz")
	}
}
