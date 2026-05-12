package workflow

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

func TestMarketplaceSyncWorkflowReplaysAppliedHistory(t *testing.T) {
	t.Parallel()

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(MarketplaceSyncWorkflow)

	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, "testdata/marketplace_sync_applied_history.json"); err != nil {
		t.Fatalf("replay marketplace sync workflow history: %v", err)
	}
}

func TestMarketplaceSyncWorkflowRetriesSyncActivityBeforeDLQ(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	attempts := 0
	env.RegisterActivityWithOptions(func(context.Context, MarketplaceSyncInput) (MarketplaceSyncResult, error) {
		attempts++
		if attempts < 3 {
			return MarketplaceSyncResult{}, errors.New("transient marketplace sync outage")
		}
		return MarketplaceSyncResult{
			Status: MarketplaceSyncWorkflowStatusDLQ,
			Sync:   marketplacesync.SyncResult{Status: marketplacesync.StatusDLQ, Attempts: attempts},
		}, nil
	}, activity.RegisterOptions{Name: MarketplaceSyncActivity})

	input := testMarketplaceSyncInput()
	env.OnActivity(MarketplaceSyncActivity, mock.Anything, input).Return(
		func(context.Context, MarketplaceSyncInput) (MarketplaceSyncResult, error) {
			attempts++
			if attempts < 3 {
				return MarketplaceSyncResult{}, errors.New("transient marketplace sync outage")
			}
			return MarketplaceSyncResult{
				Status: MarketplaceSyncWorkflowStatusDLQ,
				Event:  input.Event,
				Sync:   marketplacesync.SyncResult{Status: marketplacesync.StatusDLQ, Attempts: attempts},
			}, nil
		},
	).Times(3)

	env.ExecuteWorkflow(MarketplaceSyncWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if attempts != 3 {
		t.Fatalf("attempts = %d, want 3", attempts)
	}
	var got MarketplaceSyncResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != MarketplaceSyncWorkflowStatusDLQ {
		t.Fatalf("status = %q, want dlq", got.Status)
	}
}
