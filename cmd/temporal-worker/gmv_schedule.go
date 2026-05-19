// File scope: v6.3.0 Pair 3 MVP / CF-14 schedule registration glue.
//
// Wraps client.ScheduleClient.Create with idempotent re-registration
// semantics so the temporal-worker can call this from main without
// stacking schedules across restarts. Existing schedules are detected
// by scheduleAlreadyExists and treated as a no-op (we do not update
// the cron in place; that would require a separate
// ensure-schedule-matches-plan flow which is not in scope for v6.3.0).
package main

import (
	"context"
	"errors"
	"log/slog"
	"strings"

	ecworkflow "github.com/nfsarch33/helixon-ec/internal/workflow"
	enumspb "go.temporal.io/api/enums/v1"
	"go.temporal.io/sdk/client"
)

// ensureGMVDailyRefreshSchedule registers (or reuses) the CF-14
// schedule. Errors are logged + swallowed so a temporal-frontend
// outage does not crash the worker; the worker will exit through
// the main run loop and the next restart will retry registration.
func ensureGMVDailyRefreshSchedule(ctx context.Context, logger *slog.Logger, c client.Client, taskQueue string) {
	if c == nil || c.ScheduleClient() == nil {
		if logger != nil {
			logger.Warn("temporal_worker.gmv_schedule_skipped", "reason", "no schedule client")
		}
		return
	}
	plan := ecworkflow.GMVDailyRefreshSchedulePlan()
	if taskQueue == "" {
		taskQueue = plan.TaskQueue
	}
	_, err := c.ScheduleClient().Create(ctx, client.ScheduleOptions{
		ID: plan.ScheduleID,
		Spec: client.ScheduleSpec{
			CronExpressions: []string{plan.Cron},
			TimeZoneName:    plan.Timezone,
		},
		Action: &client.ScheduleWorkflowAction{
			ID:        plan.WorkflowID,
			Workflow:  ecworkflow.GMVDailyRefreshWorkflow,
			TaskQueue: taskQueue,
			Args:      []any{ecworkflow.GMVDailyRefreshInput{TenantScope: "*"}},
		},
		Overlap: scheduleOverlapPolicy(plan.OverlapPolicy),
	})
	switch {
	case err == nil:
		if logger != nil {
			logger.Info("temporal_worker.gmv_schedule_created", "schedule_id", plan.ScheduleID, "cron", plan.Cron, "tz", plan.Timezone)
		}
	case scheduleAlreadyExists(err):
		if logger != nil {
			logger.Info("temporal_worker.gmv_schedule_exists", "schedule_id", plan.ScheduleID)
		}
	default:
		if logger != nil {
			logger.Warn("temporal_worker.gmv_schedule_create_failed", "schedule_id", plan.ScheduleID, "error", err)
		}
	}
}

// scheduleAlreadyExists matches the canonical Temporal SDK error for
// "this schedule ID is already taken". The SDK does not export a
// typed sentinel today, so we fall back to substring matching on the
// gRPC code message ("AlreadyExists" or "already exists").
func scheduleAlreadyExists(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	if strings.Contains(msg, "AlreadyExists") || strings.Contains(strings.ToLower(msg), "already exists") {
		return true
	}
	var serviceErr interface{ GRPCStatus() any }
	return errors.As(err, &serviceErr)
}

// scheduleOverlapPolicy maps the workflow-package string constant to
// the Temporal SDK enum. Unknown values default to skip (the safest
// option for an idempotent REFRESH).
func scheduleOverlapPolicy(name string) enumspb.ScheduleOverlapPolicy {
	switch name {
	case ecworkflow.GMVDailyRefreshOverlapSkip:
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	default:
		return enumspb.SCHEDULE_OVERLAP_POLICY_SKIP
	}
}
