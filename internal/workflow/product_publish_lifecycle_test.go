package workflow

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestProductPublishWorkflowQueryIncludesLifecycleMetadata(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{Pass: true, Score: 94}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{Pass: true, Score: 100}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	env.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (PublishResult, error) {
		return PublishResult{Published: true, RemoteID: "wc-lifecycle"}, nil
	}, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	env.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})
	env.OnActivity(RecordWorkflowEventActivity, mock.Anything, mock.Anything).Return(nil).Times(5)

	var queried map[string]any
	env.RegisterDelayedCallback(func() {
		value, err := env.QueryWorkflow(ProductPublishStatusQuery)
		if err != nil {
			t.Fatalf("query workflow: %v", err)
		}
		if err := value.Get(&queried); err != nil {
			t.Fatalf("decode query result: %v", err)
		}
	}, time.Minute)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(ProductPublishReviewSignal, ReviewSignal{Approved: true, Reviewer: "lead@example.com"})
	}, 2*time.Minute)

	env.ExecuteWorkflow(ProductPublishWorkflow, ProductPublishInput{ProductID: "product-query-lifecycle"})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	if queried["status"] != ProductPublishStatusAwaitingReview {
		t.Fatalf("queried status = %#v, want %s", queried["status"], ProductPublishStatusAwaitingReview)
	}
	if queried["current_activity"] != "Awaiting human review" {
		t.Fatalf("queried current_activity = %#v, want Awaiting human review", queried["current_activity"])
	}
	if _, ok := queried["updated_at"].(string); !ok {
		t.Fatalf("queried updated_at = %#v, want timestamp string", queried["updated_at"])
	}
	activities, ok := queried["activities"].([]any)
	if !ok || len(activities) < 3 {
		t.Fatalf("queried activities = %#v, want lifecycle entries", queried["activities"])
	}
}
