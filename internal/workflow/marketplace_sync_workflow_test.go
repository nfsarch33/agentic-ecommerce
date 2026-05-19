package workflow

import (
	"context"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/marketplacesync"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestMarketplaceSyncWorkflow_DispatchesSyncActivityAndReportsStatus(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, MarketplaceSyncInput) (MarketplaceSyncResult, error) {
		return MarketplaceSyncResult{
			Status: MarketplaceSyncWorkflowStatusApplied,
			Sync:   marketplacesync.SyncResult{Status: marketplacesync.StatusApplied, RemoteID: "remote-123", Attempts: 1},
		}, nil
	}, activity.RegisterOptions{Name: MarketplaceSyncActivity})

	input := testMarketplaceSyncInput()
	env.OnActivity(MarketplaceSyncActivity, mock.Anything, input).Return(
		MarketplaceSyncResult{
			Status: MarketplaceSyncWorkflowStatusApplied,
			Sync:   marketplacesync.SyncResult{Status: marketplacesync.StatusApplied, RemoteID: "remote-123", Attempts: 1},
		}, nil,
	).Once()

	env.ExecuteWorkflow(MarketplaceSyncWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got MarketplaceSyncResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != MarketplaceSyncWorkflowStatusApplied {
		t.Fatalf("status = %q, want applied", got.Status)
	}
	if got.Sync.RemoteID != "remote-123" {
		t.Fatalf("remote id = %q, want remote-123", got.Sync.RemoteID)
	}
	value, err := env.QueryWorkflow(MarketplaceSyncStatusQuery)
	if err != nil {
		t.Fatalf("query workflow: %v", err)
	}
	var queried MarketplaceSyncResult
	if err := value.Get(&queried); err != nil {
		t.Fatalf("decode query: %v", err)
	}
	if queried.Status != MarketplaceSyncWorkflowStatusApplied {
		t.Fatalf("query status = %q, want applied", queried.Status)
	}
}

func TestMarketplaceReplayWorkflow_DispatchesReplayActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, MarketplaceReplayInput) (MarketplaceSyncResult, error) {
		return MarketplaceSyncResult{
			Status: MarketplaceSyncWorkflowStatusApplied,
			Sync:   marketplacesync.SyncResult{Status: marketplacesync.StatusApplied, RemoteID: "remote-replay", Attempts: 1},
		}, nil
	}, activity.RegisterOptions{Name: MarketplaceReplayActivity})

	input := MarketplaceReplayInput{Record: marketplacesync.DLQRecord{Event: testMarketplaceEvent(), Attempts: 3, Reason: "rate limited"}}
	env.OnActivity(MarketplaceReplayActivity, mock.Anything, input).Return(
		MarketplaceSyncResult{
			Status: MarketplaceSyncWorkflowStatusApplied,
			Sync:   marketplacesync.SyncResult{Status: marketplacesync.StatusApplied, RemoteID: "remote-replay", Attempts: 1},
		}, nil,
	).Once()

	env.ExecuteWorkflow(MarketplaceReplayWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got MarketplaceSyncResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Sync.RemoteID != "remote-replay" {
		t.Fatalf("remote id = %q, want remote-replay", got.Sync.RemoteID)
	}
}

func testMarketplaceSyncInput() MarketplaceSyncInput {
	return MarketplaceSyncInput{Event: testMarketplaceEvent()}
}

func testMarketplaceEvent() marketplacesync.ProductEvent {
	return marketplacesync.ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopify",
		EntityType: marketplacesync.EntityProduct,
		EntityID:   "sku-123",
		ExternalID: "gid://shopify/Product/1",
		Operation:  marketplacesync.OperationUpsert,
		Version:    "v1",
		Payload: map[string]any{
			"title": "Resistance Band Set",
		},
	}
}
