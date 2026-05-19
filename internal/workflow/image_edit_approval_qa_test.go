package workflow

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/media"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestImageEditApprovalWorkflowCanBeCanceledWhileAwaitingDecision(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, media.ImageEditRequest) (media.ImageEditJob, error) {
		return testImageEditJob(media.ImageEditApprovalPending), nil
	}, activity.RegisterOptions{Name: ImageEditRequestActivity})
	env.OnActivity(ImageEditRequestActivity, mock.Anything, testImageEditApprovalInput().Request).Return(
		testImageEditJob(media.ImageEditApprovalPending), nil,
	).Once()
	env.RegisterDelayedCallback(func() {
		env.CancelWorkflow()
	}, time.Minute)

	env.ExecuteWorkflow(ImageEditApprovalWorkflow, testImageEditApprovalInput())

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected workflow cancellation error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "canceled") {
		t.Fatalf("workflow error = %v, want canceled", err)
	}
}

func TestImageEditApprovalActivitiesRejectRequiresReason(t *testing.T) {
	t.Parallel()

	editor := &recordingImageEditApprovalEditor{}
	activities := NewImageEditApprovalActivities(ImageEditApprovalActivityDeps{Editor: editor})

	_, err := activities.Reject(context.Background(), ImageEditRejectionActivityInput{JobID: "image-edit-job-123"})
	if !errors.Is(err, media.ErrImageEditInvalid) {
		t.Fatalf("Reject err = %v, want ErrImageEditInvalid", err)
	}
	if editor.rejectCalled {
		t.Fatal("Reject delegated to editor before validating rejection reason")
	}
}

type recordingImageEditApprovalEditor struct {
	rejectCalled bool
}

func (e *recordingImageEditApprovalEditor) Request(context.Context, media.ImageEditRequest) (media.ImageEditJob, error) {
	return media.ImageEditJob{}, nil
}

func (e *recordingImageEditApprovalEditor) Approve(context.Context, string) (media.ImageEditJob, error) {
	return media.ImageEditJob{}, nil
}

func (e *recordingImageEditApprovalEditor) Reject(context.Context, string, string) (media.ImageEditJob, error) {
	e.rejectCalled = true
	return media.ImageEditJob{}, nil
}
