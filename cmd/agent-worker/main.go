package main

import (
	"context"
	"errors"
	"fmt"
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

	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
	complianceagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/compliance"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	pricingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/pricing"
	sourcingagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/sourcing"
)

var (
	version = "dev"
	commit  = "unknown"

	agentWorkerRunsSucceededTotal atomic.Int64
	agentWorkerRunsFailedTotal    atomic.Int64
)

// Config is the runtime contract between compose and the future v0.6.0 orchestrator.
type Config struct {
	Enabled        bool
	RunOnce        bool
	Concurrency    int
	Interval       time.Duration
	MetricsAddr    string
	EventBusDriver string
	EventBusAddr   string
	EventBusDB     string
	SyncChannel    string
	DLQChannel     string
}

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))
	if isHealthcheckArgs(os.Args) {
		if err := runHealthcheck(getenv("ECOMMERCE_AGENT_WORKER_METRICS_ADDR", "127.0.0.1:8081")); err != nil {
			logger.Error("agent-worker.healthcheck_failed", "error", err)
			os.Exit(1)
		}
		return
	}

	cfg, err := loadConfig(os.Getenv)
	if err != nil {
		logger.Error("agent-worker.invalid_config", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if err := run(ctx, logger, cfg); err != nil {
		logger.Error("agent-worker.failed", "error", err)
		os.Exit(1)
	}
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

	return Config{
		Enabled:        enabled,
		RunOnce:        runOnce,
		Concurrency:    concurrency,
		Interval:       interval,
		MetricsAddr:    getenvDefault(getenv, "ECOMMERCE_AGENT_WORKER_METRICS_ADDR", "127.0.0.1:8081"),
		EventBusDriver: getenvDefault(getenv, "ECOMMERCE_EVENTBUS_DRIVER", "redis"),
		EventBusAddr:   getenvDefault(getenv, "ECOMMERCE_EVENTBUS_REDIS_ADDR", "127.0.0.1:6379"),
		EventBusDB:     getenvDefault(getenv, "ECOMMERCE_EVENTBUS_REDIS_DB", "0"),
		SyncChannel:    getenvDefault(getenv, "ECOMMERCE_EVENTBUS_CHANNEL_SYNC", "ec.sync.events"),
		DLQChannel:     getenvDefault(getenv, "ECOMMERCE_EVENTBUS_CHANNEL_DLQ", "ec.sync.deadletter"),
	}, nil
}

func run(ctx context.Context, logger *slog.Logger, cfg Config) error {
	if !cfg.Enabled {
		logger.Info("agent-worker.disabled")
		return nil
	}
	if cfg.RunOnce {
		logger.Info("agent-worker.run_once", "concurrency", cfg.Concurrency, "eventbus_driver", cfg.EventBusDriver)
		return runScheduledJobs(ctx, logger, cfg)
	}

	httpServer := &http.Server{
		Addr:              cfg.MetricsAddr,
		Handler:           workerMux(cfg),
		ReadHeaderTimeout: 5 * time.Second,
	}
	errCh := make(chan error, 1)
	go func() {
		errCh <- httpServer.ListenAndServe()
	}()

	logger.Info(
		"agent-worker.start",
		"metrics_addr", cfg.MetricsAddr,
		"interval", cfg.Interval.String(),
		"concurrency", cfg.Concurrency,
		"eventbus_driver", cfg.EventBusDriver,
	)

	ticker := time.NewTicker(cfg.Interval)
	defer ticker.Stop()

	if err := runScheduledJobs(ctx, logger, cfg); err != nil {
		return err
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
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			return httpServer.Shutdown(shutdownCtx)
		case <-ticker.C:
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
	runtime := newOrchestratorRuntime(cfg)
	summary, err := runtime.RunOnce(ctx, logger)
	if err != nil {
		return err
	}
	logger.Info(
		"agent-worker.scheduler_cycle_complete",
		"submitted", summary.Submitted,
		"succeeded", summary.Succeeded,
		"failed", summary.Failed,
		"eventbus_driver", cfg.EventBusDriver,
		"sync_channel", cfg.SyncChannel,
		"dlq_channel", cfg.DLQChannel,
	)
	return nil
}

type workerRuntime struct {
	scheduler *orchestrator.Scheduler
	jobs      []workerJob
}

type workerJob struct {
	AgentID  string
	Priority int
	Payload  map[string]any
}

type workerRunSummary struct {
	Submitted int
	Succeeded int
	Failed    int
}

func newOrchestratorRuntime(cfg Config) workerRuntime {
	registry := orchestrator.NewRegistry()
	for _, candidate := range []orchestrator.Agent{
		complianceagent.NewAgent(),
		pricingagent.NewAgent(),
		sourcingagent.NewAgent(),
	} {
		_ = registry.Register(candidate)
	}
	return workerRuntime{
		scheduler: orchestrator.NewScheduler(
			registry,
			orchestrator.NewInMemoryStore(),
			orchestrator.NewEventRecorder(),
			nil,
			orchestrator.SchedulerOptions{MaxConcurrent: cfg.Concurrency},
		),
		jobs: []workerJob{defaultComplianceProbeJob()},
	}
}

func (r workerRuntime) RunOnce(ctx context.Context, logger *slog.Logger) (workerRunSummary, error) {
	summary := workerRunSummary{}
	for _, job := range r.jobs {
		run, err := r.scheduler.Submit(ctx, orchestrator.SubmitRequest{
			AgentID:  job.AgentID,
			Priority: job.Priority,
			Payload:  job.Payload,
		})
		if err != nil {
			return summary, err
		}
		summary.Submitted++
		completed, err := r.scheduler.Wait(ctx, run.ID)
		if err != nil {
			return summary, err
		}
		if completed.State == orchestrator.RunSucceeded {
			summary.Succeeded++
			agentWorkerRunsSucceededTotal.Add(1)
			logger.Info("agent-worker.scheduler_run_succeeded", "agent_id", completed.AgentID, "run_id", completed.ID)
			continue
		}
		summary.Failed++
		agentWorkerRunsFailedTotal.Add(1)
		logger.Error("agent-worker.scheduler_run_failed", "agent_id", completed.AgentID, "run_id", completed.ID, "state", completed.State, "error_code", completed.Error.Code)
	}
	return summary, nil
}

func defaultComplianceProbeJob() workerJob {
	output := contentagent.GeneratedContent{
		Description:     "Professional resistance training kit for home workouts, mobility drills, and progressive strength routines.",
		SEOTitle:        "Resistance Training Kit",
		MetaDescription: "A professional resistance training kit for home workouts and mobility drills.",
	}
	return workerJob{
		AgentID:  "compliance",
		Priority: 1,
		Payload: map[string]any{
			"product": map[string]any{
				"ID":          "worker-probe",
				"SKU":         "RB-SET",
				"Title":       "Resistance Band Set",
				"Description": "Resistance bands for training and mobility.",
				"Currency":    "AUD",
			},
			"output":    output,
			"style":     contentagent.StyleProfessional,
			"max_words": 80,
			"keywords":  []string{"resistance", "training"},
		},
	}
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
`, version, commit, enabled, cfg.Concurrency, cfg.Interval.Seconds(), cfg.EventBusDriver, cfg.SyncChannel, agentWorkerRunsSucceededTotal.Load(), cfg.EventBusDriver, cfg.SyncChannel, agentWorkerRunsFailedTotal.Load(), cfg.EventBusDriver, cfg.SyncChannel, cfg.EventBusDriver, cfg.SyncChannel, cfg.EventBusDriver, cfg.SyncChannel)
	}
}

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
