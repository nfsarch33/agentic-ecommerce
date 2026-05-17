//go:build generate_product_publish_history
// +build generate_product_publish_history

package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestGenerateProductPublishReplayHistories(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	server, err := testsuite.StartDevServer(ctx, testsuite.DevServerOptions{LogLevel: "error"})
	if err != nil {
		t.Fatalf("start Temporal dev server: %v", err)
	}
	defer func() {
		server.Client().Close()
		if err := server.Stop(); err != nil {
			t.Errorf("stop Temporal dev server: %v", err)
		}
	}()

	temporalWorker := worker.New(server.Client(), TaskQueue, worker.Options{})
	temporalWorker.RegisterWorkflow(ProductPublishWorkflow)
	temporalWorker.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (ComplianceResult, error) {
		return ComplianceResult{Pass: true, Score: 94}, nil
	}, activity.RegisterOptions{Name: CheckComplianceActivity})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (MediaValidationResult, error) {
		return MediaValidationResult{Pass: true, Score: 100}, nil
	}, activity.RegisterOptions{Name: ValidateMediaActivity})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, ProductPublishActivityInput) (PublishResult, error) {
		return PublishResult{Published: true, RemoteID: "wc-replay"}, nil
	}, activity.RegisterOptions{Name: PublishToWooCommerceActivity})
	temporalWorker.RegisterActivityWithOptions(func(context.Context, WorkflowEvent) error {
		return nil
	}, activity.RegisterOptions{Name: RecordWorkflowEventActivity})

	if err := temporalWorker.Start(); err != nil {
		t.Fatalf("start Temporal worker: %v", err)
	}
	defer temporalWorker.Stop()

	fixtures := []struct {
		name     string
		filename string
		review   ReviewSignal
		want     string
	}{
		{
			name:     "approved",
			filename: "product_publish_approved_history.json",
			review: ReviewSignal{
				Approved: true,
				Reviewer: "qa@example.com",
				Note:     "ready",
			},
			want: ProductPublishStatusPublished,
		},
		{
			name:     "rejected",
			filename: "product_publish_rejected_history.json",
			review: ReviewSignal{
				Approved: false,
				Reviewer: "qa@example.com",
				Note:     "copy needs work",
			},
			want: ProductPublishStatusRejected,
		},
	}

	for _, fixture := range fixtures {
		fixture := fixture
		t.Run(fixture.name, func(t *testing.T) {
			writeProductPublishHistory(ctx, t, server.Client(), fixture.filename, fixture.review, fixture.want)
		})
	}
}

func writeProductPublishHistory(
	ctx context.Context,
	t *testing.T,
	c client.Client,
	filename string,
	review ReviewSignal,
	wantStatus string,
) {
	t.Helper()

	workflowID := fmt.Sprintf("product-publish-history-%s-%d", filename, time.Now().UnixNano())
	run, err := c.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
	}, ProductPublishWorkflow, ProductPublishInput{
		ProductID:   "product-history",
		RequestedBy: "qa@example.com",
	})
	if err != nil {
		t.Fatalf("start workflow: %v", err)
	}

	waitForProductPublishStatus(ctx, t, c, run.GetID(), run.GetRunID(), ProductPublishStatusAwaitingReview)
	if err := c.SignalWorkflow(ctx, run.GetID(), run.GetRunID(), ProductPublishReviewSignal, review); err != nil {
		t.Fatalf("signal workflow review: %v", err)
	}

	var result ProductPublishResult
	if err := run.Get(ctx, &result); err != nil {
		t.Fatalf("workflow get result: %v", err)
	}
	if result.Status != wantStatus {
		t.Fatalf("workflow status = %q, want %q", result.Status, wantStatus)
	}

	historyMessage := fetchWorkflowHistory(ctx, t, c, run.GetID(), run.GetRunID())
	if historyMessage == nil || len(historyMessage.Events) == 0 {
		t.Fatal("captured empty history")
	}

	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(historyMessage)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}

	target := filepath.Join("testdata", filename)
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fmt.Println("wrote", target)
}

func waitForProductPublishStatus(
	ctx context.Context,
	t *testing.T,
	c client.Client,
	workflowID string,
	runID string,
	want string,
) ProductPublishResult {
	t.Helper()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		value, err := c.QueryWorkflow(ctx, workflowID, runID, ProductPublishStatusQuery)
		if err == nil && value != nil && value.HasValue() {
			var snapshot ProductPublishResult
			if err := value.Get(&snapshot); err == nil && snapshot.Status == want {
				return snapshot
			}
		}

		select {
		case <-ctx.Done():
			t.Fatalf("wait for status %q: %v", want, ctx.Err())
		case <-ticker.C:
		}
	}
}

func fetchWorkflowHistory(
	ctx context.Context,
	t *testing.T,
	c client.Client,
	workflowID string,
	runID string,
) *historypb.History {
	t.Helper()

	iter := c.GetWorkflowHistory(ctx, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	history := &historypb.History{}
	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			t.Fatalf("iterate workflow history: %v", err)
		}
		history.Events = append(history.Events, event)
	}
	return history
}
