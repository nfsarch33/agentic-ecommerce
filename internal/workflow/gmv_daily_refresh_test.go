package workflow

// File scope: v6.3.0 Pair 3 MVP — Story 6 / CF-14 GMV daily rollup
// REFRESH workflow tests.
//
// Coverage:
//   - happy-path REFRESH succeeds, workflow returns RefreshOK status.
//   - retryable error from activity surfaces an error result.
//   - skip-if-disabled flag short-circuits without touching the
//     activity (idempotency property).
//   - schedule plan produces an AEST 02:00 cron string suitable for
//     temporal client.ScheduleClient.Create.

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestGMVDailyRefreshWorkflow_Success(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	stub := &stubGMVActivity{result: GMVDailyRefreshResult{Status: GMVDailyRefreshStatusOK, RowsRefreshed: 1234}, err: nil}
	env.RegisterActivityWithOptions(stub.Refresh, activity.RegisterOptions{Name: GMVDailyRefreshActivity})

	env.ExecuteWorkflow(GMVDailyRefreshWorkflow, GMVDailyRefreshInput{TenantScope: "*"})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got GMVDailyRefreshResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != GMVDailyRefreshStatusOK {
		t.Fatalf("status = %s, want OK", got.Status)
	}
	if got.RowsRefreshed != 1234 {
		t.Fatalf("rows = %d, want 1234", got.RowsRefreshed)
	}
}

func TestGMVDailyRefreshWorkflow_Disabled_ShortCircuits(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	stub := &stubGMVActivity{result: GMVDailyRefreshResult{Status: GMVDailyRefreshStatusOK}}
	env.RegisterActivityWithOptions(stub.Refresh, activity.RegisterOptions{Name: GMVDailyRefreshActivity})

	env.ExecuteWorkflow(GMVDailyRefreshWorkflow, GMVDailyRefreshInput{TenantScope: "*", Disabled: true})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got GMVDailyRefreshResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != GMVDailyRefreshStatusSkipped {
		t.Fatalf("status = %s, want SKIPPED", got.Status)
	}
	if stub.calls != 0 {
		t.Fatalf("activity should not run when disabled; calls=%d", stub.calls)
	}
}

func TestGMVDailyRefreshWorkflow_ActivityError_PropagatesAfterRetries(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	stub := &stubGMVActivity{err: errors.New("boom")}
	env.RegisterActivityWithOptions(stub.Refresh, activity.RegisterOptions{Name: GMVDailyRefreshActivity})

	env.ExecuteWorkflow(GMVDailyRefreshWorkflow, GMVDailyRefreshInput{TenantScope: "*"})
	if err := env.GetWorkflowError(); err == nil {
		t.Fatalf("expected workflow error, got nil")
	}
}

func TestGMVDailyRefreshActivity_IdempotentOnNoOp(t *testing.T) {
	t.Parallel()

	exec := &stubExecutor{}
	a := NewGMVDailyRefreshActivities(GMVDailyRefreshActivityDeps{Executor: exec})
	ctx := context.Background()

	first, err := a.Refresh(ctx, GMVDailyRefreshInput{TenantScope: "*"})
	if err != nil {
		t.Fatalf("first refresh: %v", err)
	}
	second, err := a.Refresh(ctx, GMVDailyRefreshInput{TenantScope: "*"})
	if err != nil {
		t.Fatalf("second refresh: %v", err)
	}
	if first.Status != GMVDailyRefreshStatusOK || second.Status != GMVDailyRefreshStatusOK {
		t.Fatalf("idempotent retries should both succeed: first=%+v second=%+v", first, second)
	}
	if !strings.Contains(exec.lastSQL, "REFRESH MATERIALIZED VIEW CONCURRENTLY") {
		t.Fatalf("expected CONCURRENTLY refresh SQL, got %q", exec.lastSQL)
	}
}

func TestGMVDailyRefreshActivity_ExecutorError_Propagates(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{err: errors.New("db down")}
	a := NewGMVDailyRefreshActivities(GMVDailyRefreshActivityDeps{Executor: exec})

	_, err := a.Refresh(context.Background(), GMVDailyRefreshInput{TenantScope: "*"})
	if err == nil {
		t.Fatalf("expected executor error to propagate")
	}
}

func TestGMVDailyRefreshActivity_NilExecutor_Errors(t *testing.T) {
	t.Parallel()
	a := NewGMVDailyRefreshActivities(GMVDailyRefreshActivityDeps{})
	_, err := a.Refresh(context.Background(), GMVDailyRefreshInput{TenantScope: "*"})
	if err == nil {
		t.Fatalf("expected error when executor unwired")
	}
}

func TestGMVDailyRefreshActivity_CancelledContext_Errors(t *testing.T) {
	t.Parallel()
	exec := &stubExecutor{}
	a := NewGMVDailyRefreshActivities(GMVDailyRefreshActivityDeps{Executor: exec})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := a.Refresh(ctx, GMVDailyRefreshInput{TenantScope: "*"})
	if err == nil {
		t.Fatalf("expected error from cancelled context")
	}
	if exec.calls != 0 {
		t.Fatalf("executor should not be called when ctx is cancelled")
	}
}

func TestGMVDailyRefreshSchedulePlan_AEST_02_00(t *testing.T) {
	t.Parallel()
	plan := GMVDailyRefreshSchedulePlan()
	if plan.WorkflowID != GMVDailyRefreshScheduleWorkflowID {
		t.Fatalf("workflow id = %s, want %s", plan.WorkflowID, GMVDailyRefreshScheduleWorkflowID)
	}
	if plan.Cron != "0 2 * * *" {
		t.Fatalf("cron = %s, want '0 2 * * *'", plan.Cron)
	}
	if plan.Timezone != "Australia/Sydney" {
		t.Fatalf("timezone = %s, want Australia/Sydney", plan.Timezone)
	}
	if plan.TaskQueue == "" {
		t.Fatalf("task queue must be non-empty")
	}
	if plan.OverlapPolicy != GMVDailyRefreshOverlapSkip {
		t.Fatalf("overlap policy = %s, want skip", plan.OverlapPolicy)
	}
	if plan.RetentionDays < 30 {
		t.Fatalf("retention should be >= 30 days, got %d", plan.RetentionDays)
	}
	// Determinism: plan is a pure function of constants.
	if plan2 := GMVDailyRefreshSchedulePlan(); plan2 != plan {
		t.Fatalf("plan not deterministic: %+v vs %+v", plan, plan2)
	}
}

// --- helpers ---------------------------------------------------------

type stubGMVActivity struct {
	result GMVDailyRefreshResult
	err    error
	calls  int
}

func (s *stubGMVActivity) Refresh(_ context.Context, _ GMVDailyRefreshInput) (GMVDailyRefreshResult, error) {
	s.calls++
	return s.result, s.err
}

type stubExecutor struct {
	lastSQL string
	err     error
	calls   int
}

func (s *stubExecutor) ExecRefresh(_ context.Context, sql string) (int64, error) {
	s.calls++
	s.lastSQL = sql
	if s.err != nil {
		return 0, s.err
	}
	return 1234, nil
}

var _ time.Time // silence unused import on rare build tag combinations
