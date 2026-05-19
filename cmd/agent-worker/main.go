package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	agentruntime "github.com/nfsarch33/helixon-ec/internal/agent/runtime"
	"github.com/nfsarch33/helixon-ec/internal/lifecycle"
	"github.com/nfsarch33/helixon-ec/internal/memwatch"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/runtimeobs"
)

var (
	version = "dev"
	commit  = "unknown"

	agentWorkerRunsSucceededTotal    atomic.Int64
	agentWorkerRunsFailedTotal       atomic.Int64
	agentScheduledRunsSucceededTotal atomic.Int64
	agentScheduledRunsFailedTotal    atomic.Int64

	// v2.10.0 Story 4: ec_* registry mounted into the agent-worker's
	// /metrics handler when run() boots the lifecycle Manager.
	agentEcRegistry atomic.Pointer[metrics.Registry]
)

// startWorkerObservability wires the ec_* registry + memwatch sampler
// into the supplied lifecycle.Manager. Mirrors the mc-api pattern.
func startWorkerObservability(mgr *lifecycle.Manager, logger *slog.Logger, binary string) *metrics.Registry {
	rt := runtimeobs.New(logger, binary, runtimeobs.Config{
		EvomapPath:         runtimeobs.DefaultEvomapPath(os.Getenv),
		Rotate:             true,
		AgentraceJSONLPath: runtimeobs.DefaultAgentraceJSONLPath(os.Getenv),
	})
	reg := rt.Registry()
	sampler := memwatch.NewSampler(logger, memwatch.Config{
		BinaryName:        binary,
		SampleInterval:    5 * time.Second,
		Sink:              rt,
		HeapAlarmCallback: func() { reg.OOMAlarms.Inc(metrics.Labels{}) },
	})
	go func() { _ = sampler.Run(context.Background()) }()
	mgr.Register("memwatch", sampler)
	mgr.Register("runtime-observability", rt)
	return reg
}

// Config is the runtime contract between compose and the agent scheduler.
type Config struct {
	Enabled                   bool
	RunOnce                   bool
	Concurrency               int
	Interval                  time.Duration
	ScheduleEnabled           bool
	ScheduleDefaultInterval   time.Duration
	ScheduleMaxConcurrentRuns int
	ScheduleTaskQueue         string
	MetricsAddr               string
	EventBusDriver            string
	EventBusAddr              string
	EventBusDB                string
	SyncChannel               string
	DLQChannel                string
	RuntimeMode               agentruntime.Mode
}

func (c Config) resolvedRuntimeMode() agentruntime.Mode {
	if c.RuntimeMode == "" {
		return agentruntime.ModeLegacy
	}
	return c.RuntimeMode
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(mainImpl(ctx, os.Args, os.Stdout, os.Getenv))
}

// mainImpl is the testable entry point: argv + getenv + writer in,
// process exit code out. Avoids os.Exit so unit tests can drive
// happy-path, healthcheck, invalid-config, and run-error branches
// without subprocesses.
func mainImpl(ctx context.Context, args []string, stdout io.Writer, getenv func(string) string) int {
	logger := slog.New(slog.NewJSONHandler(stdout, nil))
	if isHealthcheckArgs(args) {
		addr := getenvDefault(getenv, "ECOMMERCE_AGENT_WORKER_METRICS_ADDR", "127.0.0.1:8081")
		if err := runHealthcheck(addr); err != nil {
			logger.Error("agent-worker.healthcheck_failed", "error", err)
			return 1
		}
		return 0
	}

	cfg, err := loadConfig(getenv)
	if err != nil {
		logger.Error("agent-worker.invalid_config", "error", err)
		return 1
	}

	if err := run(ctx, logger, cfg); err != nil {
		logger.Error("agent-worker.failed", "error", err)
		return 1
	}
	return 0
}

func loadConfig(getenv func(string) string) (Config, error) {
	enabled, err := parseBool(getenv("ECOMMERCE_AGENT_WORKER_ENABLED"), true)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_WORKER_ENABLED: %w", err)
	}
	runOnce, err := parseBool(getenv("ECOMMERCE_AGENT_WORKER_RUN_ONCE"), false)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_WORKER_RUN_ONCE: %w", err)
	}
	concurrency, err := parsePositiveInt(getenv("ECOMMERCE_AGENT_WORKER_CONCURRENCY"), 1)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_WORKER_CONCURRENCY: %w", err)
	}
	interval, err := parseDuration(getenv("ECOMMERCE_AGENT_WORKER_INTERVAL"), 5*time.Minute)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_WORKER_INTERVAL: %w", err)
	}
	scheduleEnabled, err := parseBool(getenv("ECOMMERCE_AGENT_SCHEDULES_ENABLED"), false)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_SCHEDULES_ENABLED: %w", err)
	}
	scheduleDefaultInterval, err := parseDuration(getenv("ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL"), 15*time.Minute)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL: %w", err)
	}
	scheduleMaxConcurrentRuns, err := parsePositiveInt(getenv("ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS"), 1)
	if err != nil {
		return Config{}, fmt.Errorf("ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS: %w", err)
	}
	runtimeMode, err := agentruntime.ParseMode(getenv("EC_AGENT_RUNTIME_MODE"))
	if err != nil {
		return Config{}, fmt.Errorf("EC_AGENT_RUNTIME_MODE: %w", err)
	}

	return Config{
		Enabled:                   enabled,
		RunOnce:                   runOnce,
		Concurrency:               concurrency,
		Interval:                  interval,
		ScheduleEnabled:           scheduleEnabled,
		ScheduleDefaultInterval:   scheduleDefaultInterval,
		ScheduleMaxConcurrentRuns: scheduleMaxConcurrentRuns,
		ScheduleTaskQueue:         firstConfigured(getenv, "ECOMMERCE_AGENT_SCHEDULES_TASK_QUEUE", "ECOMMERCE_TEMPORAL_TASK_QUEUE", "ec-workflows"),
		MetricsAddr:               getenvDefault(getenv, "ECOMMERCE_AGENT_WORKER_METRICS_ADDR", "127.0.0.1:8081"),
		EventBusDriver:            getenvDefault(getenv, "ECOMMERCE_EVENTBUS_DRIVER", "redis"),
		EventBusAddr:              getenvDefault(getenv, "ECOMMERCE_EVENTBUS_REDIS_ADDR", "127.0.0.1:6379"),
		EventBusDB:                getenvDefault(getenv, "ECOMMERCE_EVENTBUS_REDIS_DB", "0"),
		SyncChannel:               getenvDefault(getenv, "ECOMMERCE_EVENTBUS_CHANNEL_SYNC", "ec.sync.events"),
		DLQChannel:                getenvDefault(getenv, "ECOMMERCE_EVENTBUS_CHANNEL_DLQ", "ec.sync.deadletter"),
		RuntimeMode:               runtimeMode,
	}, nil
}

func run(ctx context.Context, logger *slog.Logger, cfg Config) error {
	if !cfg.Enabled {
		logger.Info("agent-worker.disabled")
		return nil
	}
	if cfg.RunOnce {
		logger.Info(
			"agent-worker.run_once",
			"concurrency", cfg.Concurrency,
			"agent_schedules_enabled", cfg.ScheduleEnabled,
			"agent_schedule_task_queue", cfg.ScheduleTaskQueue,
			"agent_runtime_mode", cfg.resolvedRuntimeMode(),
			"eventbus_driver", cfg.EventBusDriver,
		)
		if !cfg.ScheduleEnabled {
			logger.Info("agent-worker.schedules_disabled", "mode", "run_once")
			return nil
		}
		return runScheduledJobs(ctx, logger, cfg)
	}

	mgr := lifecycle.New(logger, 10*time.Second)
	reg := startWorkerObservability(mgr, logger, "agent-worker")
	agentEcRegistry.Store(reg)
	defer agentEcRegistry.Store(nil)

	httpServer := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           workerMux(cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}
	mgr.Register("metrics-server", lifecycle.CloserFunc(func(closeCtx context.Context) error {
		return httpServer.Shutdown(closeCtx)
	}))
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	logger.Info(
		"agent-worker.start",
		"metrics_addr", cfg.MetricsAddr,
		"interval", cfg.Interval.String(),
		"concurrency", cfg.Concurrency,
		"agent_schedules_enabled", cfg.ScheduleEnabled,
		"agent_schedule_default_interval", cfg.ScheduleDefaultInterval.String(),
		"agent_schedule_max_concurrent_runs", cfg.ScheduleMaxConcurrentRuns,
		"agent_schedule_task_queue", cfg.ScheduleTaskQueue,
		"agent_runtime_mode", cfg.resolvedRuntimeMode(),
		"eventbus_driver", cfg.EventBusDriver,
	)

	var tickerC <-chan time.Time
	if cfg.ScheduleEnabled {
		ticker := time.NewTicker(cfg.ScheduleDefaultInterval)
		defer ticker.Stop()
		tickerC = ticker.C
		if err := runScheduledJobs(ctx, logger, cfg); err != nil {
			return err
		}
	} else {
		logger.Info("agent-worker.schedules_disabled", "mode", "service")
	}

	for {
		select {
		case err := <-errCh:
			if err != nil && !errors.Is(err, http.ErrServerClosed) {
				return err
			}
			return nil
		case <-ctx.Done():
			logger.Info("agent-worker.shutdown")
			return mgr.Shutdown()
		case <-tickerC:
			if err := runScheduledJobs(ctx, logger, cfg); err != nil {
				return err
			}
		}
	}
}

func runScheduledJobs(ctx context.Context, logger *slog.Logger, cfg Config) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	summary, err := agentruntime.RunOnce(ctx, logger, agentruntime.Config{
		Mode:                      cfg.resolvedRuntimeMode(),
		ScheduleMaxConcurrentRuns: cfg.ScheduleMaxConcurrentRuns,
	})
	agentWorkerRunsSucceededTotal.Add(int64(summary.Succeeded))
	agentScheduledRunsSucceededTotal.Add(int64(summary.Succeeded))
	agentWorkerRunsFailedTotal.Add(int64(summary.Failed))
	agentScheduledRunsFailedTotal.Add(int64(summary.Failed))
	if err != nil {
		return err
	}
	logger.Info(
		"agent-worker.scheduler_cycle_complete",
		"submitted", summary.Submitted,
		"succeeded", summary.Succeeded,
		"failed", summary.Failed,
		"agent_runtime_mode", cfg.resolvedRuntimeMode(),
		"eventbus_driver", cfg.EventBusDriver,
		"sync_channel", cfg.SyncChannel,
		"dlq_channel", cfg.DLQChannel,
		"agent_schedule_task_queue", cfg.ScheduleTaskQueue,
	)
	return nil
}

func workerMux(cfg Config) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", healthzHandler)
	mux.HandleFunc("/metrics", metricsHandler(cfg))
	return mux
}

func healthzHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"status":"ok","service":"agentic-ecommerce-agent-worker"}`))
}

func metricsHandler(cfg Config) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		enabled := 0
		if cfg.Enabled {
			enabled = 1
		}
		schedulesEnabled := 0
		if cfg.ScheduleEnabled {
			schedulesEnabled = 1
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = fmt.Fprintf(w, `# HELP agentic_ecommerce_agent_worker_build_info Build metadata for the running agent-worker binary.
# TYPE agentic_ecommerce_agent_worker_build_info gauge
agentic_ecommerce_agent_worker_build_info{version=%q,commit=%q} 1
# HELP agentic_ecommerce_agent_worker_enabled Whether the scheduler loop is enabled.
# TYPE agentic_ecommerce_agent_worker_enabled gauge
agentic_ecommerce_agent_worker_enabled %d
# HELP agentic_ecommerce_agent_worker_concurrency Configured scheduler concurrency.
# TYPE agentic_ecommerce_agent_worker_concurrency gauge
agentic_ecommerce_agent_worker_concurrency %d
# HELP agentic_ecommerce_agent_worker_scheduler_interval_seconds Configured scheduler interval.
# TYPE agentic_ecommerce_agent_worker_scheduler_interval_seconds gauge
agentic_ecommerce_agent_worker_scheduler_interval_seconds %.0f
# HELP agentic_ecommerce_agent_schedules_enabled Whether Temporal-backed agent schedules are enabled for this deployment.
# TYPE agentic_ecommerce_agent_schedules_enabled gauge
agentic_ecommerce_agent_schedules_enabled %d
# HELP agentic_ecommerce_agent_schedule_default_interval_seconds Default interval for Temporal-backed recurring agent runs.
# TYPE agentic_ecommerce_agent_schedule_default_interval_seconds gauge
agentic_ecommerce_agent_schedule_default_interval_seconds %.0f
# HELP agentic_ecommerce_agent_schedule_max_concurrent_runs Maximum concurrent Temporal scheduled agent runs.
# TYPE agentic_ecommerce_agent_schedule_max_concurrent_runs gauge
agentic_ecommerce_agent_schedule_max_concurrent_runs %d
# HELP agentic_ecommerce_agent_schedule_config_info Temporal schedule routing metadata.
# TYPE agentic_ecommerce_agent_schedule_config_info gauge
agentic_ecommerce_agent_schedule_config_info{task_queue=%q} 1
# HELP agentic_ecommerce_agent_runtime_mode_info Selected bootstrap runtime mode for agent-worker.
# TYPE agentic_ecommerce_agent_runtime_mode_info gauge
agentic_ecommerce_agent_runtime_mode_info{mode=%q} 1
# HELP agentic_ecommerce_agent_scheduled_runs_total Temporal scheduled agent runs observed by status.
# TYPE agentic_ecommerce_agent_scheduled_runs_total counter
agentic_ecommerce_agent_scheduled_runs_total{task_queue=%q,status="succeeded"} %d
agentic_ecommerce_agent_scheduled_runs_total{task_queue=%q,status="failed"} %d
# HELP agentic_ecommerce_agent_worker_runs_total Orchestrator-backed agent runs completed by this worker.
# TYPE agentic_ecommerce_agent_worker_runs_total counter
agentic_ecommerce_agent_worker_runs_total{eventbus_driver=%q,sync_channel=%q,status="succeeded"} %d
agentic_ecommerce_agent_worker_runs_total{eventbus_driver=%q,sync_channel=%q,status="failed"} %d
# HELP agentic_ecommerce_agent_worker_compliance_checks_total Compliance checks evaluated by this worker.
# TYPE agentic_ecommerce_agent_worker_compliance_checks_total counter
agentic_ecommerce_agent_worker_compliance_checks_total{eventbus_driver=%q,sync_channel=%q} 0
# HELP agentic_ecommerce_agent_worker_compliance_failures_total Compliance checks that failed in this worker.
# TYPE agentic_ecommerce_agent_worker_compliance_failures_total counter
agentic_ecommerce_agent_worker_compliance_failures_total{eventbus_driver=%q,sync_channel=%q} 0
# HELP agentic_ecommerce_agent_worker_media_validation_failures_total Media validations rejected by this worker.
# TYPE agentic_ecommerce_agent_worker_media_validation_failures_total counter
agentic_ecommerce_agent_worker_media_validation_failures_total{eventbus_driver=%q,sync_channel=%q} 0
`, version, commit, enabled, cfg.Concurrency, cfg.Interval.Seconds(), schedulesEnabled, cfg.ScheduleDefaultInterval.Seconds(), cfg.ScheduleMaxConcurrentRuns, cfg.ScheduleTaskQueue, cfg.resolvedRuntimeMode(), cfg.ScheduleTaskQueue, agentScheduledRunsSucceededTotal.Load(), cfg.ScheduleTaskQueue, agentScheduledRunsFailedTotal.Load(), cfg.EventBusDriver, cfg.SyncChannel, agentWorkerRunsSucceededTotal.Load(), cfg.EventBusDriver, cfg.SyncChannel, agentWorkerRunsFailedTotal.Load(), cfg.EventBusDriver, cfg.SyncChannel, cfg.EventBusDriver, cfg.SyncChannel, cfg.EventBusDriver, cfg.SyncChannel)
		if reg := agentEcRegistry.Load(); reg != nil {
			reg.Handler().ServeHTTP(noopHeader{w}, r)
		}
	}
}

// noopHeader prevents the embedded ec_* handler from overwriting the
// agentic_ecommerce_* content-type header.
type noopHeader struct{ http.ResponseWriter }

func (n noopHeader) Header() http.Header { return http.Header{} }
func (n noopHeader) WriteHeader(int)     {}

func isHealthcheckArgs(args []string) bool {
	if len(args) < 2 {
		return false
	}
	return args[1] == "healthcheck" || args[1] == "--healthcheck"
}

func runHealthcheck(addr string) error {
	host, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		host = "127.0.0.1"
		port = "8081"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get("http://" + net.JoinHostPort(host, port) + "/healthz")
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("healthz status %d", resp.StatusCode)
	}
	return nil
}

func parseBool(raw string, fallback bool) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "":
		return fallback, nil
	case "1", "true", "yes", "y", "on":
		return true, nil
	case "0", "false", "no", "n", "off":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean %q", raw)
	}
}

func parsePositiveInt(raw string, fallback int) (int, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, err
	}
	if value < 1 {
		return 0, fmt.Errorf("must be >= 1")
	}
	return value, nil
}

func parseDuration(raw string, fallback time.Duration) (time.Duration, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return fallback, nil
	}
	duration, err := time.ParseDuration(raw)
	if err == nil {
		if duration <= 0 {
			return 0, fmt.Errorf("must be > 0")
		}
		return duration, nil
	}
	seconds, secondsErr := strconv.Atoi(raw)
	if secondsErr != nil {
		return 0, err
	}
	if seconds < 1 {
		return 0, fmt.Errorf("must be > 0")
	}
	return time.Duration(seconds) * time.Second, nil
}

func getenv(key, fallback string) string {
	return getenvDefault(os.Getenv, key, fallback)
}

func getenvDefault(getenv func(string) string, key, fallback string) string {
	value := strings.TrimSpace(getenv(key))
	if value == "" {
		return fallback
	}
	return value
}

func firstConfigured(getenv func(string) string, keys ...string) string {
	fallback := ""
	if len(keys) > 0 {
		fallback = keys[len(keys)-1]
		keys = keys[:len(keys)-1]
	}
	for _, key := range keys {
		if value := strings.TrimSpace(getenv(key)); value != "" {
			return value
		}
	}
	return fallback
}
