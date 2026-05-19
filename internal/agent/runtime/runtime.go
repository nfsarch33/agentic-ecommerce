package runtime

import (
	"context"
	"fmt"
	"log/slog"
	"strings"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
	complianceagent "github.com/nfsarch33/helixon-ec/internal/agent/compliance"
	contentagent "github.com/nfsarch33/helixon-ec/internal/agent/content"
	pricingagent "github.com/nfsarch33/helixon-ec/internal/agent/pricing"
	sourcingagent "github.com/nfsarch33/helixon-ec/internal/agent/sourcing"
)

type Mode string

const (
	ModeLegacy  Mode = "legacy"
	ModeShadow  Mode = "shadow"
	ModePrimary Mode = "primary"
)

type Config struct {
	Mode                      Mode
	ScheduleMaxConcurrentRuns int
}

type Summary struct {
	Submitted int
	Succeeded int
	Failed    int
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

func ParseMode(raw string) (Mode, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "", string(ModeLegacy):
		return ModeLegacy, nil
	case string(ModeShadow):
		return ModeShadow, nil
	case string(ModePrimary):
		return ModePrimary, nil
	default:
		return "", fmt.Errorf("unsupported agent runtime mode %q", raw)
	}
}

// RunOnce currently reuses the legacy scheduler path for all runtime modes.
// The mode flag exists so domains can move from legacy -> shadow -> primary
// without changing the worker contract again.
func RunOnce(ctx context.Context, logger *slog.Logger, cfg Config) (Summary, error) {
	if logger == nil {
		logger = slog.Default()
	}

	mode := cfg.Mode
	if mode == "" {
		mode = ModeLegacy
	}
	logger.Info(
		"agent-runtime.mode_selected",
		"mode", mode,
		"schedule_max_concurrent_runs", cfg.ScheduleMaxConcurrentRuns,
	)

	runtime := newWorkerRuntime(cfg)
	summary := Summary{}
	for _, job := range runtime.jobs {
		run, err := runtime.scheduler.Submit(ctx, orchestrator.SubmitRequest{
			AgentID:  job.AgentID,
			Priority: job.Priority,
			Payload:  job.Payload,
		})
		if err != nil {
			return summary, err
		}
		summary.Submitted++

		completed, err := runtime.scheduler.Wait(ctx, run.ID)
		if err != nil {
			return summary, err
		}
		if completed.State == orchestrator.RunSucceeded {
			summary.Succeeded++
			logger.Info("agent-worker.scheduler_run_succeeded", "agent_id", completed.AgentID, "run_id", completed.ID)
			continue
		}

		summary.Failed++
		logger.Error(
			"agent-worker.scheduler_run_failed",
			"agent_id", completed.AgentID,
			"run_id", completed.ID,
			"state", completed.State,
			"error_code", completed.Error.Code,
		)
	}

	return summary, nil
}

func newWorkerRuntime(cfg Config) workerRuntime {
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
			orchestrator.SchedulerOptions{MaxConcurrent: cfg.ScheduleMaxConcurrentRuns},
		),
		jobs: []workerJob{defaultComplianceProbeJob()},
	}
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
