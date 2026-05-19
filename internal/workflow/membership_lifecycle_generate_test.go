//go:build generate_membership_history
// +build generate_membership_history

// This file is a one-shot generator for the membership lifecycle replay
// fixture. It is intentionally gated behind the `generate_membership_history`
// build tag so the regular `go test` matrix never runs it.
//
// Regenerate the fixture with:
//
//	go test -tags generate_membership_history -run TestGenerateMembershipLifecycleHistory ./internal/workflow/...
//
// The output lands at testdata/membership_lifecycle_history.json and is
// then exercised by TestMembershipLifecycleWorkflowReplaysGoldenHistory
// during normal test runs.
package workflow

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/notification"
	stripeadapter "github.com/nfsarch33/agentic-ecommerce/internal/adapter/stripe"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	enumspb "go.temporal.io/api/enums/v1"
	historypb "go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestGenerateMembershipLifecycleHistory(t *testing.T) {
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

	gateway := stripeadapter.NewPaymentGateway(stripeadapter.Config{
		Clock: fixedClock{now: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)},
	})
	notifier := notification.NewMembershipNotificationRecorder()
	activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{
		Gateway: gateway, Notifier: notifier,
	})

	temporalWorker := worker.New(server.Client(), TaskQueue, worker.Options{})
	temporalWorker.RegisterWorkflow(MembershipLifecycleWorkflow)
	temporalWorker.RegisterActivityWithOptions(activities.ChargeStripe, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	temporalWorker.RegisterActivityWithOptions(activities.SendNotification, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	temporalWorker.RegisterActivityWithOptions(activities.RecordBillingEvent, activity.RegisterOptions{Name: MembershipRecordBillingActivity})
	if err := temporalWorker.Start(); err != nil {
		t.Fatalf("start Temporal worker: %v", err)
	}
	defer temporalWorker.Stop()

	subID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a5")
	memberID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a6")
	planID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a7")
	input := MembershipLifecycleInput{
		TenantID:       "tenant-a",
		SubscriptionID: subID,
		MemberID:       memberID,
		MemberEmail:    "alice@example.com",
		PlanID:         planID,
		PlanName:       "Gold",
		BillingCycle:   membership.BillingCycleMonthly,
		StripePriceID:  "price_dev_1",
		TrialDays:      7,
	}

	workflowID := fmt.Sprintf("membership-lifecycle-history-%d", time.Now().UnixNano())
	run, err := server.Client().ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID:        workflowID,
		TaskQueue: TaskQueue,
	}, MembershipLifecycleWorkflow, input)
	if err != nil {
		t.Fatalf("start workflow: %v", err)
	}

	// Wait for trial period to begin, then cancel.
	waitForMembershipStatus(ctx, t, server.Client(), run.GetID(), run.GetRunID(), membership.StateTrial)
	if err := server.Client().SignalWorkflow(ctx, run.GetID(), run.GetRunID(), MembershipCancelSignal, "qa-generator"); err != nil {
		t.Fatalf("signal cancel: %v", err)
	}

	var result MembershipLifecycleResult
	if err := run.Get(ctx, &result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.FinalState != membership.StateCancelled {
		t.Fatalf("want final state cancelled, got %s", result.FinalState)
	}

	historyMsg := fetchMembershipHistory(ctx, t, server.Client(), run.GetID(), run.GetRunID())
	if historyMsg == nil || len(historyMsg.Events) == 0 {
		t.Fatal("captured empty history")
	}

	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(historyMsg)
	if err != nil {
		t.Fatalf("marshal history: %v", err)
	}

	target := filepath.Join("testdata", "membership_lifecycle_history.json")
	if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(target, append(out, '\n'), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	fmt.Println("wrote", target)
}

func waitForMembershipStatus(
	ctx context.Context,
	t *testing.T,
	c client.Client,
	workflowID, runID string,
	want membership.State,
) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for {
		value, err := c.QueryWorkflow(ctx, workflowID, runID, MembershipStatusQuery)
		if err == nil && value != nil && value.HasValue() {
			var snapshot MembershipLifecycleResult
			if err := value.Get(&snapshot); err == nil && snapshot.FinalState == want {
				return
			}
		}
		select {
		case <-ctx.Done():
			t.Fatalf("wait for state %q: %v", want, ctx.Err())
		case <-ticker.C:
		}
	}
}

func fetchMembershipHistory(
	ctx context.Context,
	t *testing.T,
	c client.Client,
	workflowID, runID string,
) *historypb.History {
	t.Helper()
	iter := c.GetWorkflowHistory(ctx, workflowID, runID, false, enumspb.HISTORY_EVENT_FILTER_TYPE_ALL_EVENT)
	h := &historypb.History{}
	for iter.HasNext() {
		event, err := iter.Next()
		if err != nil {
			t.Fatalf("iterate workflow history: %v", err)
		}
		h.Events = append(h.Events, event)
	}
	return h
}
