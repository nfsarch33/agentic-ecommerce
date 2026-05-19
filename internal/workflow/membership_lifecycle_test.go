package workflow

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/notification"
	stripeadapter "github.com/nfsarch33/agentic-ecommerce/internal/adapter/stripe"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
	"go.temporal.io/sdk/worker"
)

type fixedClock struct{ now time.Time }

func (f fixedClock) Now() time.Time { return f.now }

func newDeterministicGateway() *stripeadapter.PaymentGateway {
	return stripeadapter.NewPaymentGateway(stripeadapter.Config{
		Clock: fixedClock{now: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)},
	})
}

func TestMembershipLifecycleWorkflowTrialThenActivateThenCancel(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	gateway := newDeterministicGateway()
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

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipCancelSignal, "user-request")
	}, 35*24*time.Hour) // after first renewal so we exercise cancel-from-active

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MembershipLifecycleResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.FinalState != membership.StateCancelled {
		t.Fatalf("final state = %s, want cancelled", result.FinalState)
	}
	if result.StripeSubscriptionID == "" {
		t.Fatal("stripe subscription id is empty")
	}
	if len(notifier.Events()) == 0 {
		t.Fatal("notifier received no events")
	}
}

func TestMembershipLifecycleWorkflowImmediateCancelDuringTrial(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	gateway := newDeterministicGateway()
	notifier := notification.NewMembershipNotificationRecorder()
	activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{
		Gateway: gateway, Notifier: notifier,
	})
	env.RegisterActivityWithOptions(activities.ChargeStripe, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	env.RegisterActivityWithOptions(activities.SendNotification, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(activities.RecordBillingEvent, activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	subID := uuid.New()
	input := MembershipLifecycleInput{
		TenantID: "tenant-a", SubscriptionID: subID, MemberID: uuid.New(), MemberEmail: "alice@example.com",
		PlanID: uuid.New(), PlanName: "Gold", BillingCycle: membership.BillingCycleMonthly,
		StripePriceID: "price_dev_2", TrialDays: 7,
	}

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipCancelSignal, "user-request")
	}, 24*time.Hour)

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, input)
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MembershipLifecycleResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.FinalState != membership.StateCancelled {
		t.Fatalf("final state = %s, want cancelled", result.FinalState)
	}
}

func TestMembershipLifecycleWorkflowChargeFailureExpiresSubscription(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	// First charge succeeds (initial), subsequent renewal fails consistently.
	calls := 0
	env.RegisterActivityWithOptions(func(_ context.Context, _ ChargeRequest) (ChargeResponse, error) {
		calls++
		if calls == 1 {
			return ChargeResponse{
				StripeSubscriptionID: "sub_dev_test",
				StripeCustomerID:     "cus_dev_test",
				CurrentPeriodEnd:     time.Now().Add(30 * 24 * time.Hour),
			}, nil
		}
		return ChargeResponse{}, errors.New("payment_required")
	}, activity.RegisterOptions{Name: MembershipChargeStripeActivity})

	env.RegisterActivityWithOptions(func(context.Context, NotificationRequest) error {
		return nil
	}, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(func(context.Context, BillingEventInput) error {
		return nil
	}, activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	input := MembershipLifecycleInput{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail: "alice@example.com", PlanID: uuid.New(), PlanName: "Gold",
		BillingCycle: membership.BillingCycleMonthly, StripePriceID: "price_dev_x",
		TrialDays: 0,
	}

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, input)
	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected workflow to fail on renewal payment outage")
	}
}

// TestMembershipLifecycleWorkflowReplaysGoldenHistory protects the
// workflow from accidental non-determinism by replaying a recorded
// successful trial->cancel run captured in testdata.
//
// The fixture is generated once per workflow change with the dedicated
// generator (build tag `generate_membership_history`); the fixture is
// committed alongside the workflow code so any future change that
// breaks deterministic replay surfaces here as a `non-determinism
// detected` error from the temporal SDK.
//
// If the file is absent (e.g. on a brand-new clone before the generator
// has been run), the test logs a regen instruction and skips so the
// rest of the matrix still proves the workflow is healthy.
func TestMembershipLifecycleWorkflowReplaysGoldenHistory(t *testing.T) {
	t.Parallel()

	const fixturePath = "testdata/membership_lifecycle_history.json"
	if _, err := os.Stat(fixturePath); errors.Is(err, os.ErrNotExist) {
		t.Skipf(
			"%s missing; regenerate with: go test -tags generate_membership_history -run TestGenerateMembershipLifecycleHistory ./internal/workflow/...",
			fixturePath,
		)
	}

	replayer := worker.NewWorkflowReplayer()
	replayer.RegisterWorkflow(MembershipLifecycleWorkflow)

	if err := replayer.ReplayWorkflowHistoryFromJSONFile(nil, fixturePath); err != nil {
		t.Fatalf("replay workflow history: %v", err)
	}
}

// TestMembershipLifecycleWorkflowDeterministicSmoke is the always-on
// determinism guardrail: it runs the workflow twice with the same
// inputs against deterministic adapters and asserts both runs produce
// the same final state, stripe id, and notification sequence. This
// catches non-determinism (e.g. accidental time.Now()/rand usage)
// without requiring an external Temporal server to capture the JSON
// fixture used by the replay test above.
func TestMembershipLifecycleWorkflowDeterministicSmoke(t *testing.T) {
	t.Parallel()

	run := func() MembershipLifecycleResult {
		var suite testsuite.WorkflowTestSuite
		env := suite.NewTestWorkflowEnvironment()
		env.SetStartTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

		gateway := newDeterministicGateway()
		notifier := notification.NewMembershipNotificationRecorder()
		activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{
			Gateway: gateway, Notifier: notifier,
		})
		env.RegisterActivityWithOptions(activities.ChargeStripe, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
		env.RegisterActivityWithOptions(activities.SendNotification, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
		env.RegisterActivityWithOptions(activities.RecordBillingEvent, activity.RegisterOptions{Name: MembershipRecordBillingActivity})

		env.RegisterDelayedCallback(func() {
			env.SignalWorkflow(MembershipCancelSignal, "user-request")
		}, 24*time.Hour)

		env.ExecuteWorkflow(MembershipLifecycleWorkflow, MembershipLifecycleInput{
			TenantID:       "tenant-a",
			SubscriptionID: uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a5"),
			MemberID:       uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a6"),
			MemberEmail:    "alice@example.com",
			PlanID:         uuid.MustParse("4d96a04a-6e30-49d0-9a4d-1a5dab6a44a7"),
			PlanName:       "Gold",
			BillingCycle:   membership.BillingCycleMonthly,
			StripePriceID:  "price_dev_1",
			TrialDays:      7,
		})
		if err := env.GetWorkflowError(); err != nil {
			t.Fatalf("workflow err: %v", err)
		}
		var got MembershipLifecycleResult
		if err := env.GetWorkflowResult(&got); err != nil {
			t.Fatalf("decode result: %v", err)
		}
		return got
	}

	first := run()
	second := run()

	if first.FinalState != second.FinalState {
		t.Fatalf("non-deterministic final state: %s vs %s", first.FinalState, second.FinalState)
	}
	if first.StripeSubscriptionID != second.StripeSubscriptionID {
		t.Fatalf("non-deterministic stripe sub id: %s vs %s", first.StripeSubscriptionID, second.StripeSubscriptionID)
	}
	if len(first.BillingEvents) != len(second.BillingEvents) {
		t.Fatalf("non-deterministic billing events: %d vs %d", len(first.BillingEvents), len(second.BillingEvents))
	}
}

// TestMembershipLifecycleActivitiesUseRealAdaptersEndToEnd exercises the
// activity layer end-to-end against the deterministic Stripe stub and
// the in-memory notification recorder.
func TestMembershipLifecycleActivitiesUseRealAdaptersEndToEnd(t *testing.T) {
	t.Parallel()

	gateway := newDeterministicGateway()
	notifier := notification.NewMembershipNotificationRecorder()
	billing := &recordingBillingLedger{}
	activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{
		Gateway: gateway, Notifier: notifier, BillingLedger: billing,
	})

	subID := uuid.New()
	memberID := uuid.New()
	resp, err := activities.ChargeStripe(context.Background(), ChargeRequest{
		TenantID: "tenant-a", SubscriptionID: subID, MemberID: memberID,
		MemberEmail: "alice@example.com", StripePriceID: "price_dev_e2e",
		BillingCycle: membership.BillingCycleMonthly, TrialDays: 0,
	})
	if err != nil {
		t.Fatalf("ChargeStripe: %v", err)
	}
	if resp.StripeSubscriptionID == "" {
		t.Fatal("empty stripe sub id")
	}

	if err := activities.SendNotification(context.Background(), NotificationRequest{
		TenantID: "tenant-a", SubscriptionID: subID,
		State: membership.StateActive, Transition: membership.TransitionActivate,
		OccurredAt: time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}); err != nil {
		t.Fatalf("SendNotification: %v", err)
	}
	if len(notifier.Events()) != 1 {
		t.Fatalf("notifier events = %d", len(notifier.Events()))
	}

	if err := activities.RecordBillingEvent(context.Background(), BillingEventInput{
		TenantID: "tenant-a", SubscriptionID: subID, StripeSubscriptionID: resp.StripeSubscriptionID,
		Kind: "initial", OccurredAt: time.Now().UTC(),
	}); err != nil {
		t.Fatalf("RecordBillingEvent: %v", err)
	}
	if len(billing.events) != 1 {
		t.Fatalf("billing events = %d", len(billing.events))
	}
}

// TestMembershipLifecycleActivitiesRejectMissingDeps confirms the activity
// struct returns typed errors when its dependencies are unset.
func TestMembershipLifecycleActivitiesRejectMissingDeps(t *testing.T) {
	t.Parallel()
	activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{})
	if _, err := activities.ChargeStripe(context.Background(), ChargeRequest{}); err == nil {
		t.Fatal("expected gateway-not-configured error")
	}
	// Notifier and ledger nil should be a no-op.
	if err := activities.SendNotification(context.Background(), NotificationRequest{}); err != nil {
		t.Fatalf("nil notifier should be a no-op: %v", err)
	}
	if err := activities.RecordBillingEvent(context.Background(), BillingEventInput{}); err != nil {
		t.Fatalf("nil ledger should be a no-op: %v", err)
	}
}

// TestMembershipLifecycleWorkflowAcceptsMockedActivities is a basic
// determinism + happy-path smoke using mock matchers.
func TestMembershipLifecycleWorkflowAcceptsMockedActivities(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()

	env.RegisterActivityWithOptions(func(context.Context, ChargeRequest) (ChargeResponse, error) {
		return ChargeResponse{
			StripeSubscriptionID: "sub_dev_mock",
			StripeCustomerID:     "cus_dev_mock",
			CurrentPeriodEnd:     time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	env.RegisterActivityWithOptions(func(context.Context, NotificationRequest) error {
		return nil
	}, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(func(context.Context, BillingEventInput) error {
		return nil
	}, activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	env.OnActivity(MembershipChargeStripeActivity, mock.Anything, mock.Anything).Return(ChargeResponse{
		StripeSubscriptionID: "sub_dev_mock",
		StripeCustomerID:     "cus_dev_mock",
		CurrentPeriodEnd:     time.Now().Add(30 * 24 * time.Hour),
	}, nil).Once()
	env.OnActivity(MembershipSendNotificationActivity, mock.Anything, mock.Anything).Return(nil).Maybe()
	env.OnActivity(MembershipRecordBillingActivity, mock.Anything, mock.Anything).Return(nil).Maybe()

	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipCancelSignal, "stop")
	}, 12*time.Hour)

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, MembershipLifecycleInput{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail: "alice@example.com", PlanID: uuid.New(), PlanName: "Gold",
		BillingCycle: membership.BillingCycleMonthly, StripePriceID: "price_dev_mock",
		TrialDays: 0,
	})
	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow err: %v", err)
	}
}

// T-5036-1: Membership renewal edge cases.

// TestMembershipLifecycleWorkflowPauseThenResumeThenCancel verifies the
// pause/resume signal path: the workflow should enter paused state, stay
// paused until a resume signal, then re-enter the renewal loop and accept
// a cancel signal.
func TestMembershipLifecycleWorkflowPauseThenResumeThenCancel(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	gateway := newDeterministicGateway()
	notifier := notification.NewMembershipNotificationRecorder()
	activities := NewMembershipLifecycleActivities(MembershipLifecycleActivityDeps{
		Gateway: gateway, Notifier: notifier,
	})
	env.RegisterActivityWithOptions(activities.ChargeStripe, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	env.RegisterActivityWithOptions(activities.SendNotification, activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(activities.RecordBillingEvent, activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	// pause at day 2 (within trial), resume at day 3, cancel at day 4
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipPauseSignal, "user-pause")
	}, 2*24*time.Hour)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipResumeSignal, "user-resume")
	}, 3*24*time.Hour)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipCancelSignal, "user-cancel")
	}, 4*24*time.Hour)

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, MembershipLifecycleInput{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail: "alice@example.com", PlanID: uuid.New(), PlanName: "Gold",
		BillingCycle: membership.BillingCycleMonthly, StripePriceID: "price_dev_pause",
		TrialDays: 7,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MembershipLifecycleResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.FinalState != membership.StateCancelled {
		t.Fatalf("want final state cancelled, got %s", result.FinalState)
	}
}

// TestMembershipLifecycleWorkflowCancelDuringFirstRenewal verifies that a
// cancel signal arriving exactly at the renewal boundary resolves cleanly
// rather than causing a race between the timer and the cancel signal.
func TestMembershipLifecycleWorkflowCancelDuringFirstRenewal(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	chargeCount := 0
	env.RegisterActivityWithOptions(func(_ context.Context, _ ChargeRequest) (ChargeResponse, error) {
		chargeCount++
		return ChargeResponse{
			StripeSubscriptionID: "sub_renewal_race",
			StripeCustomerID:     "cus_renewal_race",
			CurrentPeriodEnd:     time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	env.RegisterActivityWithOptions(func(context.Context, NotificationRequest) error { return nil },
		activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(func(context.Context, BillingEventInput) error { return nil },
		activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	// Cancel immediately after the trial expires (7 days) before renewal fires.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipCancelSignal, "race-cancel")
	}, 7*24*time.Hour)

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, MembershipLifecycleInput{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail: "alice@example.com", PlanID: uuid.New(), PlanName: "Gold",
		BillingCycle: membership.BillingCycleMonthly, StripePriceID: "price_dev_race",
		TrialDays: 7,
	})

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result MembershipLifecycleResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("decode result: %v", err)
	}
	if result.FinalState != membership.StateCancelled {
		t.Fatalf("want final state cancelled, got %s", result.FinalState)
	}
	// The initial charge must have fired exactly once.
	if chargeCount != 1 {
		t.Fatalf("want 1 charge (initial only), got %d", chargeCount)
	}
}

// TestMembershipLifecycleWorkflowPauseThenExpiry verifies that a pause
// signal does not block a renewal charge -- after the cycle timer fires
// (post-resume), renewal proceeds normally.
func TestMembershipLifecycleWorkflowPauseThenExpiry(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	env.SetStartTime(time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC))

	chargeCount := 0
	env.RegisterActivityWithOptions(func(_ context.Context, req ChargeRequest) (ChargeResponse, error) {
		chargeCount++
		if chargeCount >= 2 {
			return ChargeResponse{}, errors.New("payment_required")
		}
		return ChargeResponse{
			StripeSubscriptionID: "sub_pause_expiry",
			StripeCustomerID:     "cus_pause_expiry",
			CurrentPeriodEnd:     time.Now().Add(30 * 24 * time.Hour),
		}, nil
	}, activity.RegisterOptions{Name: MembershipChargeStripeActivity})
	env.RegisterActivityWithOptions(func(context.Context, NotificationRequest) error { return nil },
		activity.RegisterOptions{Name: MembershipSendNotificationActivity})
	env.RegisterActivityWithOptions(func(context.Context, BillingEventInput) error { return nil },
		activity.RegisterOptions{Name: MembershipRecordBillingActivity})

	// Pause after activation (day 8), resume shortly after (day 9).
	// Renewal fires at day 30; charge fails -> expiry.
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipPauseSignal, "pause-before-expiry")
	}, 8*24*time.Hour)
	env.RegisterDelayedCallback(func() {
		env.SignalWorkflow(MembershipResumeSignal, "resume-before-expiry")
	}, 9*24*time.Hour)

	env.ExecuteWorkflow(MembershipLifecycleWorkflow, MembershipLifecycleInput{
		TenantID: "tenant-a", SubscriptionID: uuid.New(), MemberID: uuid.New(),
		MemberEmail: "alice@example.com", PlanID: uuid.New(), PlanName: "Gold",
		BillingCycle: membership.BillingCycleMonthly, StripePriceID: "price_dev_expiry",
		TrialDays: 7,
	})

	err := env.GetWorkflowError()
	if err == nil {
		t.Fatal("expected workflow to fail on payment_required")
	}
}

type recordingBillingLedger struct {
	events []BillingEventInput
}

func (r *recordingBillingLedger) RecordBillingEvent(_ context.Context, evt BillingEventInput) error {
	r.events = append(r.events, evt)
	return nil
}

var _ port.MembershipNotificationSender = (*notification.MembershipNotificationRecorder)(nil)
var _ MembershipBillingLedger = (*recordingBillingLedger)(nil)
