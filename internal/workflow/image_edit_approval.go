package workflow

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	ImageEditRequestActivity = "image_edit.request"
	ImageEditApproveActivity = "image_edit.approve"
	ImageEditRejectActivity  = "image_edit.reject"

	ImageEditApprovalSignal      = "image-edit-approval"
	ImageEditApprovalUpdate      = "image-edit-approval-update"
	ImageEditApprovalStatusQuery = "image-edit-approval-status"
)

type ImageEditApprovalInput struct {
	Request     media.ImageEditRequest `json:"request"`
	RequestedBy string                 `json:"requested_by,omitempty"`
}

type ImageEditApprovalDecision struct {
	Approved bool   `json:"approved"`
	Reviewer string `json:"reviewer,omitempty"`
	Reason   string `json:"reason,omitempty"`
}

type ImageEditApprovalResult struct {
	Status   string                    `json:"status"`
	Job      media.ImageEditJob        `json:"job"`
	Decision ImageEditApprovalDecision `json:"decision,omitempty"`
}

type ImageEditApprovalActivityInput struct {
	JobID string `json:"job_id"`
}

type ImageEditRejectionActivityInput struct {
	JobID  string `json:"job_id"`
	Reason string `json:"reason,omitempty"`
}

type ImageEditApprovalEditor interface {
	Request(context.Context, media.ImageEditRequest) (media.ImageEditJob, error)
	Approve(context.Context, string) (media.ImageEditJob, error)
	Reject(context.Context, string, string) (media.ImageEditJob, error)
}

type ImageEditApprovalActivityDeps struct {
	Editor ImageEditApprovalEditor
}

type ImageEditApprovalActivities struct {
	editor ImageEditApprovalEditor
}

func NewImageEditApprovalActivities(deps ImageEditApprovalActivityDeps) *ImageEditApprovalActivities {
	return &ImageEditApprovalActivities{editor: deps.Editor}
}

func ImageEditApprovalWorkflow(ctx temporalworkflow.Context, input ImageEditApprovalInput) (ImageEditApprovalResult, error) {
	ctx = temporalworkflow.WithActivityOptions(ctx, imageEditApprovalActivityOptions())
	state := ImageEditApprovalResult{Status: string(media.ImageEditApprovalRequested)}
	var decision ImageEditApprovalDecision
	decisionReceived := false

	if err := temporalworkflow.SetQueryHandler(ctx, ImageEditApprovalStatusQuery, func() (ImageEditApprovalResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}
	if err := temporalworkflow.SetUpdateHandler(ctx, ImageEditApprovalUpdate, func(ctx temporalworkflow.Context, in ImageEditApprovalDecision) (ImageEditApprovalResult, error) {
		if err := validateImageEditApprovalDecision(in); err != nil {
			return state, err
		}
		decision = in
		decisionReceived = true
		state.Decision = in
		return state, nil
	}); err != nil {
		return state, err
	}

	if err := temporalworkflow.ExecuteActivity(ctx, ImageEditRequestActivity, input.Request).Get(ctx, &state.Job); err != nil {
		return state, err
	}
	state.Status = string(state.Job.ApprovalState)
	if state.Job.ApprovalState != media.ImageEditApprovalPending {
		return state, nil
	}

	if err := awaitImageEditApprovalDecision(ctx, &decision, &decisionReceived); err != nil {
		return state, err
	}
	state.Decision = decision
	if decision.Approved {
		return approveImageEditJob(ctx, state)
	}
	return rejectImageEditJob(ctx, state)
}

func imageEditApprovalActivityOptions() temporalworkflow.ActivityOptions {
	return temporalworkflow.ActivityOptions{
		StartToCloseTimeout: 5 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
}

func awaitImageEditApprovalDecision(ctx temporalworkflow.Context, decision *ImageEditApprovalDecision, decisionReceived *bool) error {
	signalCh := temporalworkflow.GetSignalChannel(ctx, ImageEditApprovalSignal)
	temporalworkflow.Go(ctx, func(ctx temporalworkflow.Context) {
		for !*decisionReceived {
			var in ImageEditApprovalDecision
			signalCh.Receive(ctx, &in)
			if err := validateImageEditApprovalDecision(in); err != nil {
				continue
			}
			*decision = in
			*decisionReceived = true
			return
		}
	})
	if err := temporalworkflow.Await(ctx, func() bool { return *decisionReceived }); err != nil {
		return temporal.NewCanceledError("image edit approval canceled while awaiting decision")
	}
	return nil
}

func approveImageEditJob(ctx temporalworkflow.Context, state ImageEditApprovalResult) (ImageEditApprovalResult, error) {
	if err := temporalworkflow.ExecuteActivity(ctx, ImageEditApproveActivity, ImageEditApprovalActivityInput{JobID: state.Job.ID}).Get(ctx, &state.Job); err != nil {
		return state, err
	}
	state.Status = string(state.Job.ApprovalState)
	return state, nil
}

func rejectImageEditJob(ctx temporalworkflow.Context, state ImageEditApprovalResult) (ImageEditApprovalResult, error) {
	input := ImageEditRejectionActivityInput{JobID: state.Job.ID, Reason: state.Decision.Reason}
	if err := temporalworkflow.ExecuteActivity(ctx, ImageEditRejectActivity, input).Get(ctx, &state.Job); err != nil {
		return state, err
	}
	state.Status = string(state.Job.ApprovalState)
	return state, nil
}

func validateImageEditApprovalDecision(in ImageEditApprovalDecision) error {
	if strings.TrimSpace(in.Reviewer) == "" {
		return errors.New("image edit approval reviewer is required")
	}
	if !in.Approved && strings.TrimSpace(in.Reason) == "" {
		return errors.New("image edit rejection reason is required")
	}
	return nil
}

func (a *ImageEditApprovalActivities) Request(ctx context.Context, req media.ImageEditRequest) (media.ImageEditJob, error) {
	if a.editor == nil {
		return media.ImageEditJob{}, errors.New("image edit approval editor is not configured")
	}
	return a.editor.Request(ctx, req)
}

func (a *ImageEditApprovalActivities) Approve(ctx context.Context, input ImageEditApprovalActivityInput) (media.ImageEditJob, error) {
	if a.editor == nil {
		return media.ImageEditJob{}, errors.New("image edit approval editor is not configured")
	}
	if strings.TrimSpace(input.JobID) == "" {
		return media.ImageEditJob{}, fmt.Errorf("%w: job id required", media.ErrImageEditInvalid)
	}
	return a.editor.Approve(ctx, input.JobID)
}

func (a *ImageEditApprovalActivities) Reject(ctx context.Context, input ImageEditRejectionActivityInput) (media.ImageEditJob, error) {
	if a.editor == nil {
		return media.ImageEditJob{}, errors.New("image edit approval editor is not configured")
	}
	if strings.TrimSpace(input.JobID) == "" {
		return media.ImageEditJob{}, fmt.Errorf("%w: job id required", media.ErrImageEditInvalid)
	}
	if strings.TrimSpace(input.Reason) == "" {
		return media.ImageEditJob{}, fmt.Errorf("%w: rejection reason required", media.ErrImageEditInvalid)
	}
	return a.editor.Reject(ctx, input.JobID, input.Reason)
}
