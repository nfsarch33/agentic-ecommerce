package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	agentruntime "github.com/nfsarch33/helixon-ec/internal/agent/runtime"
)

func TestLoadConfigDefaultsRuntimeModeToLegacy(t *testing.T) {
	t.Parallel()

	cfg, err := loadConfig(func(string) string { return "" })
	if err != nil {
		t.Fatalf("loadConfig returned error: %v", err)
	}

	if got := cfg.resolvedRuntimeMode(); got != agentruntime.ModeLegacy {
		t.Fatalf("runtime mode = %q, want legacy", got)
	}
}

func TestLoadConfigRejectsInvalidRuntimeMode(t *testing.T) {
	t.Parallel()

	_, err := loadConfig(func(key string) string {
		if key == "EC_AGENT_RUNTIME_MODE" {
			return "surprise"
		}
		return ""
	})
	if err == nil {
		t.Fatal("loadConfig returned nil error for invalid runtime mode")
	}
	if !strings.Contains(err.Error(), "EC_AGENT_RUNTIME_MODE") {
		t.Fatalf("loadConfig error = %q, want EC_AGENT_RUNTIME_MODE prefix", err)
	}
}

func TestMetricsHandlerExposesRuntimeModeInfo(t *testing.T) {
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
		RuntimeMode:               agentruntime.ModeShadow,
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()

	metricsHandler(cfg)(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `agentic_ecommerce_agent_runtime_mode_info{mode="shadow"} 1`) {
		t.Fatalf("metrics missing runtime mode info:\n%s", rec.Body.String())
	}
}
