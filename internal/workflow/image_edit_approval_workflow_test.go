package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/media"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestImageEditApprovalWorkflow_WaitsForApprovalSignalBeforeApproveActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerImageEditApprovalTestActivities(env)

	input := testImageEditApprovalInput()
	requested := testImageEditJob(media.ImageEditApprovalPending)
	approved := testImageEditJob(media.ImageEditApprovalApproved)
	env.OnActivity(ImageEditRequestActivity, mock.Anything, input.Request).Return(requested, nil).Once()
	env.OnActivity(ImageEditApproveActivity, mock.Anything, ImageEditApprovalActivityInput{JobID: requested.ID}).Return(approved, nil).Once()

	var queried ImageEditApprovalResult
	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(ImageEditApprovalStatusQuery)
		if err != nil {
			t.Fatalf("query workflow: %v", err)
		}
		if err := value.Get(&queried); err != nil {
			t.Fatalf("decode query: %v", err)
		}
	}, time.Minute)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ImageEditApprovalSignal, ImageEditApprovalDecision{Approved: true, Reviewer: "lead@example.com"})
	}, 2*time.Minute)

	env.ExecuteWorkflow(ImageEditApprovalWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got ImageEditApprovalResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != string(media.ImageEditApprovalApproved) {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	if queried.Status != string(media.ImageEditApprovalPending) {
		t.Fatalf("query status = %q, want pending approval", queried.Status)
	}
	if got.Decision.Reviewer != "lead@example.com" {
		t.Fatalf("decision = %+v, want reviewer from signal", got.Decision)
	}
}

func TestImageEditApprovalWorkflow_AcceptsApprovalUpdate(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerImageEditApprovalTestActivities(env)

	input := testImageEditApprovalInput()
	requested := testImageEditJob(media.ImageEditApprovalPending)
	approved := testImageEditJob(media.ImageEditApprovalApproved)
	env.OnActivity(ImageEditRequestActivity, mock.Anything, input.Request).Return(requested, nil).Once()
	env.OnActivity(ImageEditApproveActivity, mock.Anything, ImageEditApprovalActivityInput{JobID: requested.ID}).Return(approved, nil).Once()

	env.RegisterDelayedCallback(func() {
		env.UpdateWorkflowNoRejection(
			ImageEditApprovalUpdate,
			"approve-update",
			t,
			ImageEditApprovalDecision{Approved: true, Reviewer: "lead@example.com"},
		)
	}, time.Minute)

	env.ExecuteWorkflow(ImageEditApprovalWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got ImageEditApprovalResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != string(media.ImageEditApprovalApproved) {
		t.Fatalf("status = %q, want approved", got.Status)
	}
	if got.Decision.Reviewer != "lead@example.com" {
		t.Fatalf("decision = %+v, want update reviewer", got.Decision)
	}
}

func TestImageEditApprovalWorkflow_RejectionSignalCallsRejectActivity(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerImageEditApprovalTestActivities(env)

	input := testImageEditApprovalInput()
	requested := testImageEditJob(media.ImageEditApprovalPending)
	rejected := testImageEditJob(media.ImageEditApprovalRejected)
	env.OnActivity(ImageEditRequestActivity, mock.Anything, input.Request).Return(requested, nil).Once()
	env.OnActivity(ImageEditRejectActivity, mock.Anything, ImageEditRejectionActivityInput{JobID: requested.ID, Reason: "brand mismatch"}).Return(rejected, nil).Once()
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ImageEditApprovalSignal, ImageEditApprovalDecision{Approved: false, Reviewer: "lead@example.com", Reason: "brand mismatch"})
	}, time.Minute)

	env.ExecuteWorkflow(ImageEditApprovalWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var got ImageEditApprovalResult
	if err := env.GetWorkflowResult(&got); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if got.Status != string(media.ImageEditApprovalRejected) {
		t.Fatalf("status = %q, want rejected", got.Status)
	}
}

func registerImageEditApprovalTestActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, media.ImageEditRequest) (media.ImageEditJob, error) {
		return media.ImageEditJob{}, nil
	}, activity.RegisterOptions{Name: ImageEditRequestActivity})
	env.RegisterActivityWithOptions(func(context.Context, ImageEditApprovalActivityInput) (media.ImageEditJob, error) {
		return media.ImageEditJob{}, nil
	}, activity.RegisterOptions{Name: ImageEditApproveActivity})
	env.RegisterActivityWithOptions(func(context.Context, ImageEditRejectionActivityInput) (media.ImageEditJob, error) {
		return media.ImageEditJob{}, nil
	}, activity.RegisterOptions{Name: ImageEditRejectActivity})
}

func testImageEditApprovalInput() ImageEditApprovalInput {
	return ImageEditApprovalInput{
		Request: media.ImageEditRequest{
			TenantID:           "tenant-a",
			ProductID:          "product-123",
			SourceURI:          "s3://media/source.jpg",
			Prompt:             "generate marketplace-ready product hero image",
			Action:             media.ImageEditActionLifestyleGeneration,
			SourceBytes:        4 * 1024 * 1024,
			RequiresApproval:   true,
			PreferredProviders: []string{"fleet-image-bridge"},
		},
		RequestedBy: "operator@example.com",
	}
}

func testImageEditJob(state media.ImageEditApprovalState) media.ImageEditJob {
	return media.ImageEditJob{
		ID:            "image-edit-job-123",
		TenantID:      "tenant-a",
		ProductID:     "product-123",
		Action:        media.ImageEditActionLifestyleGeneration,
		SourceURI:     "s3://media/source.jpg",
		Provider:      "fleet-image-bridge",
		OutputURI:     "s3://media/result.jpg",
		ApprovalState: state,
		SourceBytes:   4 * 1024 * 1024,
	}
}
