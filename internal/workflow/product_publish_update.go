package workflow

import (
	"context"
	"fmt"
	"time"

	temporal "go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"

	"github.com/nfsarch33/agentic-ecommerce/internal/publishgate"
)

// ReviewUpdateInput carries one review decision submitted through the
// Update path (the second decision's UpdateID, for the record).
type ReviewUpdateInput struct {
	Approved bool
	Reviewer string
	Reason   string
	UpdateID string
}

// ReviewUpdateResult is what the Update returns to mc-api.
type ReviewUpdateResult struct {
	Accepted bool
	Review   ReviewSignal
	Err      string
}

// reviewUpdateHandler is the Update-side review gate: exactly one decision
// per workflow, refused thereafter. The handler runs on the workflow's event
// loop; a decision (from the Update or the deprecated signal) is delivered to
// the blocked main loop through decidedCh, so no polling timers are needed
// and pre-gate recorded histories (signal-only) still replay.
type reviewUpdateHandler struct {
	decidedCh temporalworkflow.Channel
	review    ReviewSignal
	updateID  string
}

// install wires the product-publish-review Update with a validator that
// refuses any second decision.
func installReviewUpdate(ctx temporalworkflow.Context, h *reviewUpdateHandler) error {
	h.decidedCh = temporalworkflow.NewChannel(ctx)
	return temporalworkflow.SetUpdateHandlerWithOptions(ctx, "product-publish-review",
		func(ctx temporalworkflow.Context, in ReviewUpdateInput) (ReviewUpdateResult, error) {
			if h.review.Reviewer != "" || h.updateID != "" {
				return ReviewUpdateResult{
					Accepted: false,
					Review:   h.review,
					Err:      fmt.Sprintf("already_decided: %t by %s", h.review.Approved, h.review.Reviewer),
				}, temporal.NewNonRetryableApplicationError("already decided", "ReviewAlreadyDecided", nil)
			}
			h.updateID = in.UpdateID
			h.review = ReviewSignal{Approved: in.Approved, Reviewer: in.Reviewer, Note: in.Reason}
			h.decidedCh.Send(ctx, h.review)
			return ReviewUpdateResult{Accepted: true, Review: h.review}, nil
		},
		temporalworkflow.UpdateHandlerOptions{
			Validator: func(ctx temporalworkflow.Context, in ReviewUpdateInput) error {
				if h.review.Reviewer != "" || h.updateID != "" {
					return temporal.NewNonRetryableApplicationError("already decided", "ReviewAlreadyDecided", nil)
				}
				return nil
			},
		},
	)
}

// waitReviewDecision blocks until a decision arrives through the Update
// handler (preferred) or the deprecated signal (in-flight workflows). Both
// deliver onto the same channel, so the blocking shape matches the recorded
// pre-gate histories.
func waitReviewDecision(ctx temporalworkflow.Context, h *reviewUpdateHandler) (ReviewSignal, error) {
	var review ReviewSignal
	selector := temporalworkflow.NewSelector(ctx)
	signalCh := temporalworkflow.GetSignalChannel(ctx, ProductPublishReviewSignal)
	delivered := false
	selector.AddReceive(signalCh, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		ch.Receive(ctx, &review)
		if h.review.Reviewer == "" && h.updateID == "" {
			h.review = review
		}
		delivered = true
	})
	selector.AddReceive(h.decidedCh, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		ch.Receive(ctx, &review)
		delivered = true
	})
	selector.AddReceive(ctx.Done(), func(temporalworkflow.ReceiveChannel, bool) {
		delivered = true
	})
	selector.Select(ctx)
	if ctx.Err() != nil {
		return ReviewSignal{}, temporal.NewCanceledError("product publish canceled while awaiting review")
	}
	_ = delivered
	return h.review, nil
}

// viaUpdate reports whether the recorded decision arrived through the Update
// path (deprecated signal deliveries keep the pre-gate event sequence, so
// histories recorded before the gate replay unchanged - D2).
func (h *reviewUpdateHandler) viaUpdate() bool { return h.updateID != "" }

// workflowWithActivityOpts applies the timeouts the record activity needs.
func workflowWithActivityOpts(ctx temporalworkflow.Context, timeout time.Duration) temporalworkflow.Context {
	return temporalworkflow.WithActivityOptions(ctx, temporalworkflow.ActivityOptions{
		StartToCloseTimeout: timeout,
	})
}

// workflowGetID returns the running workflow's id (its executions are keyed
// per draft revision, and the approval row is keyed on it).
func workflowGetID(ctx temporalworkflow.Context) string {
	info := temporalworkflow.GetInfo(ctx)
	return info.WorkflowExecution.ID
}

// RecordApprovalInput is the activity payload.
type RecordApprovalInput struct {
	WorkflowID string
	TenantID   string
	ProductID  string
	Approved   bool
	Reviewer   string
	Reason     string
	UpdateID   string
}

// RecordApproval registers with the worker under RecordApprovalActivityName
// and persists the decision BEFORE any publish (D3): the publish gate fails
// closed without this row.
func (a *ProductPublishActivities) RecordApproval(ctx context.Context, input RecordApprovalInput) error {
	if a.Approvals == nil {
		return fmt.Errorf("approval recorder not configured")
	}
	_, _, err := a.Approvals.RecordApproval(ctx, publishgate.Decision{
		WorkflowID: input.WorkflowID,
		TenantID:   input.TenantID,
		ProductID:  input.ProductID,
		Approved:   input.Approved,
		Actor:      input.Reviewer,
		Reason:     input.Reason,
		UpdateID:   input.UpdateID,
	})
	return err
}

// RecordApprovalActivityName const lives in product_publish.go alongside the
// other activity names.
