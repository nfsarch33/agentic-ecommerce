package workflow

import (
	"context"
	"errors"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/marketplacesync"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	MarketplaceSyncActivity    = "marketplace_sync.sync"
	MarketplaceReplayActivity  = "marketplace_sync.replay"
	MarketplaceSyncStatusQuery = "marketplace-sync-status"

	MarketplaceSyncWorkflowStatusRunning   = "running"
	MarketplaceSyncWorkflowStatusApplied   = "applied"
	MarketplaceSyncWorkflowStatusDuplicate = "duplicate"
	MarketplaceSyncWorkflowStatusDLQ       = "dlq"
	MarketplaceSyncWorkflowStatusFailed    = "failed"
)

type MarketplaceSyncInput struct {
	Event marketplacesync.ProductEvent `json:"event"`
}

type MarketplaceReplayInput struct {
	Record marketplacesync.DLQRecord `json:"record"`
}

type MarketplaceSyncResult struct {
	Status string                       `json:"status"`
	Event  marketplacesync.ProductEvent `json:"event"`
	Sync   marketplacesync.SyncResult   `json:"sync"`
}

type MarketplaceSyncExecutor interface {
	Sync(context.Context, marketplacesync.ProductEvent) (marketplacesync.SyncResult, error)
	Replay(context.Context, marketplacesync.DLQRecord) (marketplacesync.SyncResult, error)
}

type MarketplaceSyncActivityDeps struct {
	Executor MarketplaceSyncExecutor
}

type MarketplaceSyncActivities struct {
	executor MarketplaceSyncExecutor
}

func NewMarketplaceSyncActivities(deps MarketplaceSyncActivityDeps) *MarketplaceSyncActivities {
	return &MarketplaceSyncActivities{executor: deps.Executor}
}

func MarketplaceSyncWorkflow(ctx temporalworkflow.Context, input MarketplaceSyncInput) (MarketplaceSyncResult, error) {
	ctx = temporalworkflow.WithActivityOptions(ctx, marketplaceSyncActivityOptions())
	state := MarketplaceSyncResult{Status: MarketplaceSyncWorkflowStatusRunning, Event: input.Event}
	if err := temporalworkflow.SetQueryHandler(ctx, MarketplaceSyncStatusQuery, func() (MarketplaceSyncResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	if err := temporalworkflow.ExecuteActivity(ctx, MarketplaceSyncActivity, input).Get(ctx, &state); err != nil {
		state.Status = MarketplaceSyncWorkflowStatusFailed
		return state, err
	}
	if state.Status == "" {
		state.Status = marketplaceSyncWorkflowStatus(state.Sync.Status)
	}
	return state, nil
}

func MarketplaceReplayWorkflow(ctx temporalworkflow.Context, input MarketplaceReplayInput) (MarketplaceSyncResult, error) {
	ctx = temporalworkflow.WithActivityOptions(ctx, marketplaceSyncActivityOptions())
	state := MarketplaceSyncResult{Status: MarketplaceSyncWorkflowStatusRunning, Event: input.Record.Event}
	if err := temporalworkflow.SetQueryHandler(ctx, MarketplaceSyncStatusQuery, func() (MarketplaceSyncResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	if err := temporalworkflow.ExecuteActivity(ctx, MarketplaceReplayActivity, input).Get(ctx, &state); err != nil {
		state.Status = MarketplaceSyncWorkflowStatusFailed
		return state, err
	}
	if state.Status == "" {
		state.Status = marketplaceSyncWorkflowStatus(state.Sync.Status)
	}
	return state, nil
}

func marketplaceSyncActivityOptions() temporalworkflow.ActivityOptions {
	return temporalworkflow.ActivityOptions{
		StartToCloseTimeout: 2 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
}

func (a *MarketplaceSyncActivities) Sync(ctx context.Context, input MarketplaceSyncInput) (MarketplaceSyncResult, error) {
	if a.executor == nil {
		return MarketplaceSyncResult{}, errors.New("marketplace sync executor is not configured")
	}
	result, err := a.executor.Sync(ctx, input.Event)
	return MarketplaceSyncResult{
		Status: marketplaceSyncWorkflowStatus(result.Status),
		Event:  input.Event,
		Sync:   result,
	}, err
}

func (a *MarketplaceSyncActivities) Replay(ctx context.Context, input MarketplaceReplayInput) (MarketplaceSyncResult, error) {
	if a.executor == nil {
		return MarketplaceSyncResult{}, errors.New("marketplace replay executor is not configured")
	}
	result, err := a.executor.Replay(ctx, input.Record)
	return MarketplaceSyncResult{
		Status: marketplaceSyncWorkflowStatus(result.Status),
		Event:  input.Record.Event,
		Sync:   result,
	}, err
}

func marketplaceSyncWorkflowStatus(status marketplacesync.SyncStatus) string {
	switch status {
	case marketplacesync.StatusApplied:
		return MarketplaceSyncWorkflowStatusApplied
	case marketplacesync.StatusDuplicate:
		return MarketplaceSyncWorkflowStatusDuplicate
	case marketplacesync.StatusDLQ:
		return MarketplaceSyncWorkflowStatusDLQ
	default:
		return MarketplaceSyncWorkflowStatusFailed
	}
}
