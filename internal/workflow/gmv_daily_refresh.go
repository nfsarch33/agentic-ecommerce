// Package workflow gmv_daily_refresh.go — v6.3.0 Pair 3 MVP /
// CF-14: Temporal scheduled workflow + activity that refreshes the
// gmv_daily_rollup materialized view nightly at 02:00 Australia/
// Sydney time.
//
// Design:
//
//   - GMVDailyRefreshWorkflow is a deterministic workflow that
//     dispatches a single activity. It supports a Disabled flag so a
//     scheduled run can be skipped without deregistering the schedule
//     (operational kill-switch). Status outcomes are bounded
//     constants so dashboard panels stay stable.
//
//   - GMVDailyRefreshActivities.Refresh is the side-effecting
//     activity. It calls a small RefreshExecutor port that production
//     wires to a postgres pool; tests wire a stub. The activity is
//     idempotent: running it twice in a row produces the same view
//     state (the materialized view is rebuilt from the orders
//     source-of-truth either way), so retries are safe.
//
//   - GMVDailyRefreshSchedulePlan is a pure function returning the
//     deterministic schedule plan (cron, timezone, overlap policy,
//     retention) that the temporal-worker registers via
//     client.ScheduleClient.Create at startup. Pure-function so the
//     plan can be unit-tested without standing up a Temporal
//     frontend, satisfying the workflowcheck determinism gate.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT regress
// from 5):
//   - GMVDailyRefreshWorkflow            cyclomatic 3
//   - GMVDailyRefreshActivities.Refresh  cyclomatic 4
//   - GMVDailyRefreshSchedulePlan        cyclomatic 1
package workflow

import (
	"context"
	"fmt"
	"time"

	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

// CF-14 typed activity name + status constants.
const (
	// GMVDailyRefreshActivity is the registered activity name. Used
	// by both the worker registration and the workflow dispatch so
	// the wire identifier is single-sourced.
	GMVDailyRefreshActivity = "gmv.daily_refresh"

	GMVDailyRefreshStatusOK      = "ok"
	GMVDailyRefreshStatusSkipped = "skipped"

	// GMVDailyRefreshScheduleID is the canonical schedule ID under
	// which the temporal-worker registers the cron. Stable across
	// restarts so re-registration is idempotent.
	GMVDailyRefreshScheduleID         = "gmv-daily-refresh"
	GMVDailyRefreshScheduleWorkflowID = "gmv-daily-refresh-workflow"

	// GMVDailyRefreshOverlapSkip ensures a slow refresh is not
	// stacked on the next 02:00 trigger; safer than retry-when-busy
	// because the materialized view rebuild is the slowest part of
	// the path.
	GMVDailyRefreshOverlapSkip = "skip"
)

// gmvRefreshSQL is the canonical SQL the activity runs. CONCURRENTLY
// is mandatory so the daily rebuild does not lock the dashboard
// reader (the unique index on (tenant_id, channel, day) introduced
// in migration 0016 is the prerequisite).
const gmvRefreshSQL = "REFRESH MATERIALIZED VIEW CONCURRENTLY gmv_daily_rollup"

// GMVDailyRefreshInput is the workflow input. TenantScope is "*" for
// the canonical refresh (the materialized view is shared across
// tenants); a future per-tenant variant would set TenantScope to the
// tenant ID.
type GMVDailyRefreshInput struct {
	TenantScope string `json:"tenant_scope"`
	Disabled    bool   `json:"disabled,omitempty"`
}

// GMVDailyRefreshResult is the workflow + activity result.
type GMVDailyRefreshResult struct {
	Status        string `json:"status"`
	RowsRefreshed int64  `json:"rows_refreshed"`
}

// RefreshExecutor is the small port the activity executes the
// REFRESH SQL through. Production wires a pgxpool-backed adapter;
// tests wire a stub.
type RefreshExecutor interface {
	ExecRefresh(ctx context.Context, sql string) (int64, error)
}

// GMVDailyRefreshActivityDeps wires the activity struct.
type GMVDailyRefreshActivityDeps struct {
	Executor RefreshExecutor
}

// GMVDailyRefreshActivities is the activity surface registered with
// the worker.
type GMVDailyRefreshActivities struct {
	executor RefreshExecutor
}

// NewGMVDailyRefreshActivities constructs the activity surface.
func NewGMVDailyRefreshActivities(deps GMVDailyRefreshActivityDeps) *GMVDailyRefreshActivities {
	return &GMVDailyRefreshActivities{executor: deps.Executor}
}

// Refresh runs the canonical REFRESH MATERIALIZED VIEW CONCURRENTLY
// statement. The activity is idempotent because the materialized
// view rebuilds deterministically from the orders source-of-truth.
// Retry surface (set by the workflow): up to 3 attempts with
// exponential backoff.
func (a *GMVDailyRefreshActivities) Refresh(ctx context.Context, _ GMVDailyRefreshInput) (GMVDailyRefreshResult, error) {
	if err := ctx.Err(); err != nil {
		return GMVDailyRefreshResult{}, err
	}
	if a.executor == nil {
		return GMVDailyRefreshResult{}, fmt.Errorf("gmv refresh: executor unwired")
	}
	rows, err := a.executor.ExecRefresh(ctx, gmvRefreshSQL)
	if err != nil {
		return GMVDailyRefreshResult{}, fmt.Errorf("gmv refresh: %w", err)
	}
	return GMVDailyRefreshResult{Status: GMVDailyRefreshStatusOK, RowsRefreshed: rows}, nil
}

// GMVDailyRefreshWorkflow dispatches the refresh activity. It is
// registered with the temporal-worker and triggered by the schedule
// returned from GMVDailyRefreshSchedulePlan.
func GMVDailyRefreshWorkflow(ctx temporalworkflow.Context, input GMVDailyRefreshInput) (GMVDailyRefreshResult, error) {
	if input.Disabled {
		return GMVDailyRefreshResult{Status: GMVDailyRefreshStatusSkipped}, nil
	}
	options := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: 30 * time.Minute, // CONCURRENTLY can take a while at scale.
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    10 * time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    2 * time.Minute,
			MaximumAttempts:    3,
		},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, options)

	var result GMVDailyRefreshResult
	if err := temporalworkflow.ExecuteActivity(ctx, GMVDailyRefreshActivity, input).Get(ctx, &result); err != nil {
		return GMVDailyRefreshResult{}, err
	}
	return result, nil
}

// SchedulePlan describes the schedule the temporal-worker registers
// via client.ScheduleClient.Create. Pure function -- no I/O, no SDK
// imports beyond constants -- so the plan can be unit-tested
// without standing up a Temporal frontend.
type SchedulePlan struct {
	ScheduleID    string
	WorkflowID    string
	WorkflowType  string
	TaskQueue     string
	Cron          string
	Timezone      string
	OverlapPolicy string
	RetentionDays int
}

// GMVDailyRefreshSchedulePlan returns the canonical CF-14 schedule
// plan: nightly 02:00 Australia/Sydney (AEST/AEDT), skip on overlap,
// 90 day retention.
func GMVDailyRefreshSchedulePlan() SchedulePlan {
	return SchedulePlan{
		ScheduleID:    GMVDailyRefreshScheduleID,
		WorkflowID:    GMVDailyRefreshScheduleWorkflowID,
		WorkflowType:  "GMVDailyRefreshWorkflow",
		TaskQueue:     TaskQueue,
		Cron:          "0 2 * * *",
		Timezone:      "Australia/Sydney",
		OverlapPolicy: GMVDailyRefreshOverlapSkip,
		RetentionDays: 90,
	}
}
