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
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/adapter/notification"
	stripeadapter "github.com/nfsarch33/helixon-ec/internal/adapter/stripe"
	"github.com/nfsarch33/helixon-ec/internal/domain/membership"
	"go.temporal.io/api/history/v1"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"google.golang.org/protobuf/encoding/protojson"
)

func TestGenerateMembershipLifecycleHistory(t *testing.T) {
	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	gateway := stripeadapter.NewPaymentGateway(stripeadapter.Config{
		Clock: fixedClock{now: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)},
	})
	notifier := notification.NewMembershipNotificationRecorder()
	activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{
		Gateway: gateway, Notifier: notifier,
	})
	env.RegisterActivityWithOptions(activities.ChargeStripe, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	env.RegisterActivityWithOptions(activities.SendNotification, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(activities.RecordBillingEvent, activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	subID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a5")
	memberID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a6")
	planID := uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a7")

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipCancelSignal, "user-request")
	}, 24*time.Hour)

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, MembershipLifecycleInput{
		TenantID:       "tenant-a",
		SubscriptionID: subID,
		MemberID:       memberID,
		MemberEmail:    "alice@example.com",
		PlanID:         planID,
		PlanName:       "Gold",
		BillingCycle:   membership.BillingCycleMonthly,
		StripePriceID:  "price_dev_1",
		TrialDays:      7,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}

	historyMessage := captureHistory(t, env)
	if historyMessage == nil || len(historyMessage.Events) == 0 {
		t.Fatal("captured empty history")
	}

	out, err := protojson.MarshalOptions{Multiline: true, Indent: "  "}.Marshal(historyMessage)
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

// captureHistory reaches into the testsuite environment to extract the
// recorded history protobuf. The temporal SDK exposes this via the
// internal worker so the call may evolve; if it does, this generator is
// the only place that needs an update.
func captureHistory(t *testing.T, env *testsuite.TestWorkflowEnvironment) *history.History {
	t.Helper()
	type historyAccessor interface {
		GetWorkflowHistory() *history.History
	}
	if accessor, ok := any(env).(historyAccessor); ok {
		return accessor.GetWorkflowHistory()
	}
	t.Fatalf("temporal SDK testsuite no longer exposes GetWorkflowHistory; %v", errors.New("update generator"))
	return nil
}

// silence the import in non-build-tag mode
var _ = context.Background
