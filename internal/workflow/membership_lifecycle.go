package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/membership"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

// Membership lifecycle workflow constants. Activity names are stable to
// keep replay tests hermetic.
const (
	MembershipChargeStripeActivity     = "membership.charge_stripe"
	MembershipSendNotificationActivity = "membership.send_notification"
	MembershipRecordBillingActivity    = "membership.record_billing_event"
	MembershipCancelSignal             = "membership.cancel"
	MembershipPauseSignal              = "membership.pause"
	MembershipResumeSignal             = "membership.resume"
	MembershipStatusQuery              = "membership.status"
)

// MembershipLifecycleInput is the workflow input.
type MembershipLifecycleInput struct {
	TenantID       string                  `json:"tenant_id"`
	SubscriptionID uuid.UUID               `json:"subscription_id"`
	MemberID       uuid.UUID               `json:"member_id"`
	MemberEmail    string                  `json:"member_email"`
	PlanID         uuid.UUID               `json:"plan_id"`
	PlanName       string                  `json:"plan_name"`
	BillingCycle   membership.BillingCycle `json:"billing_cycle"`
	StripePriceID  string                  `json:"stripe_price_id"`
	TrialDays      int                     `json:"trial_days"`
}

// MembershipLifecycleResult is the final workflow result.
type MembershipLifecycleResult struct {
	TenantID             string               `json:"tenant_id"`
	SubscriptionID       uuid.UUID            `json:"subscription_id"`
	FinalState           membership.State     `json:"final_state"`
	StripeSubscriptionID string               `json:"stripe_subscription_id"`
	BillingEvents        []BillingEventRecord `json:"billing_events"`
	Notifications        []NotificationRecord `json:"notifications"`
}

// ChargeRequest is the activity input that drives Stripe.
type ChargeRequest struct {
	TenantID       string                  `json:"tenant_id"`
	SubscriptionID uuid.UUID               `json:"subscription_id"`
	MemberID       uuid.UUID               `json:"member_id"`
	MemberEmail    string                  `json:"member_email"`
	StripePriceID  string                  `json:"stripe_price_id"`
	BillingCycle   membership.BillingCycle `json:"billing_cycle"`
	TrialDays      int                     `json:"trial_days"`
	IsRenewal      bool                    `json:"is_renewal"`
}

// ChargeResponse is the activity output from Stripe.
type ChargeResponse struct {
	StripeSubscriptionID string    `json:"stripe_subscription_id"`
	StripeCustomerID     string    `json:"stripe_customer_id"`
	CurrentPeriodEnd     time.Time `json:"current_period_end"`
}

// NotificationRequest is the activity input for the notification sender.
type NotificationRequest struct {
	TenantID       string                `json:"tenant_id"`
	SubscriptionID uuid.UUID             `json:"subscription_id"`
	MemberID       uuid.UUID             `json:"member_id"`
	MemberEmail    string                `json:"member_email"`
	PlanID         uuid.UUID             `json:"plan_id"`
	PlanName       string                `json:"plan_name"`
	State          membership.State      `json:"state"`
	Transition     membership.Transition `json:"transition"`
	OccurredAt     time.Time             `json:"occurred_at"`
}

// NotificationRecord is a deterministic, JSON-friendly summary of a
// single sent notification, returned to the workflow result.
type NotificationRecord struct {
	State      membership.State      `json:"state"`
	Transition membership.Transition `json:"transition"`
	OccurredAt time.Time             `json:"occurred_at"`
}

// BillingEventRecord is the JSON-friendly billing event summary.
type BillingEventRecord struct {
	StripeSubscriptionID string    `json:"stripe_subscription_id"`
	OccurredAt           time.Time `json:"occurred_at"`
	Kind                 string    `json:"kind"` // initial|renewal|cancellation
}

// BillingEventInput is the activity input that records a billing event
// on the membership-side ledger. Kept separate from ChargeResponse so
// the activity can be a no-op in dev.
type BillingEventInput struct {
	TenantID             string    `json:"tenant_id"`
	SubscriptionID       uuid.UUID `json:"subscription_id"`
	StripeSubscriptionID string    `json:"stripe_subscription_id"`
	Kind                 string    `json:"kind"`
	OccurredAt           time.Time `json:"occurred_at"`
}

// MembershipLifecycleActivityDeps wires concrete dependencies into the
// activity struct.
type MembershipLifecycleActivityDeps struct {
	Gateway       port.MembershipPaymentGateway
	Notifier      port.MembershipNotificationSender
	BillingLedger MembershipBillingLedger
}

// MembershipBillingLedger is the optional ledger the workflow uses to
// record charge/refund attempts. Implementations may be a no-op.
type MembershipBillingLedger interface {
	RecordBillingEvent(ctx context.Context, evt BillingEventInput) error
}

// MembershipLifecycleActivities is the activity struct the worker
// registers.
type MembershipLifecycleActivities struct {
	gateway       port.MembershipPaymentGateway
	notifier      port.MembershipNotificationSender
	billingLedger MembershipBillingLedger
}

// NewMembershipLifecycleActivities constructs the activity struct.
func NewMembershipLifecycleActivities(deps MembershipLifecycleActivityDeps) *MembershipLifecycleActivities {
	return &MembershipLifecycleActivities{
		gateway:       deps.Gateway,
		notifier:      deps.Notifier,
		billingLedger: deps.BillingLedger,
	}
}

// MembershipLifecycleWorkflow drives a single subscription through
// trial -> active -> renewal cycle -> expiry, honouring cancel/pause/
// resume signals. All side effects go through activities; workflow code
// is deterministic (no time.Now, no rand, no map iteration that
// influences control flow).
func MembershipLifecycleWorkflow(ctx temporalworkflow.Context, input MembershipLifecycleInput) (MembershipLifecycleResult, error) {
	ctx = temporalworkflow.WithActivityOptions(ctx, membershipLifecycleActivityOptions())
	state := initialMembershipLifecycleState(input)
	if err := temporalworkflow.SetQueryHandler(ctx, MembershipStatusQuery, func() (MembershipLifecycleResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	signals := membershipLifecycleSignals{
		cancel: temporalworkflow.GetSignalChannel(ctx, MembershipCancelSignal),
		pause:  temporalworkflow.GetSignalChannel(ctx, MembershipPauseSignal),
		resume: temporalworkflow.GetSignalChannel(ctx, MembershipResumeSignal),
	}

	charge, err := startMembershipLifecycle(ctx, input, &state)
	if err != nil {
		return state, err
	}
	done, err := waitForTrialCompletion(ctx, input, &state, charge, signals.cancel)
	if err != nil {
		return state, err
	}
	if done {
		return state, nil
	}
	if err := activateMembership(ctx, input, &state); err != nil {
		return state, err
	}
	return state, runMembershipRenewalLoop(ctx, input, &state, charge, signals)
}

func membershipLifecycleActivityOptions() temporalworkflow.ActivityOptions {
	return temporalworkflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
}

func initialMembershipLifecycleState(input MembershipLifecycleInput) MembershipLifecycleResult {
	return MembershipLifecycleResult{
		TenantID:       input.TenantID,
		SubscriptionID: input.SubscriptionID,
		FinalState:     membership.StateTrial,
	}
}

type membershipLifecycleSignals struct {
	cancel temporalworkflow.ReceiveChannel
	pause  temporalworkflow.ReceiveChannel
	resume temporalworkflow.ReceiveChannel
}

func startMembershipLifecycle(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
) (ChargeResponse, error) {
	// Initial charge through the gateway. Even a trial subscription
	// records a deterministic stripe id so the rest of the workflow can
	// emit billing events keyed off it.
	charge, err := executeCharge(ctx, input, false)
	if err != nil {
		return ChargeResponse{}, err
	}
	state.StripeSubscriptionID = charge.StripeSubscriptionID
	if err := recordMembershipBillingEvent(ctx, input, state, charge, "initial"); err != nil {
		return ChargeResponse{}, err
	}
	if err := notifyMembershipState(ctx, input, state, membership.StateTrial, membership.TransitionActivate); err != nil {
		return ChargeResponse{}, err
	}
	return charge, nil
}

func waitForTrialCompletion(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
	charge ChargeResponse,
	cancelSignal temporalworkflow.ReceiveChannel,
) (bool, error) {
	if input.TrialDays <= 0 {
		return false, nil
	}
	trial, _ := waitForCancelOrTimer(ctx, time.Duration(input.TrialDays)*24*time.Hour, cancelSignal)
	if trial {
		return false, nil
	}
	return true, cancelMembership(ctx, input, state, charge.StripeSubscriptionID)
}

func activateMembership(ctx temporalworkflow.Context, input MembershipLifecycleInput, state *MembershipLifecycleResult) error {
	return notifyMembershipState(ctx, input, state, membership.StateActive, membership.TransitionActivate)
}

func runMembershipRenewalLoop(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
	charge ChargeResponse,
	signals membershipLifecycleSignals,
) error {
	pauseRequested := false
	cycleEnd := temporalworkflow.Now(ctx).Add(input.BillingCycle.Duration())
	for {
		shouldContinue, transition, paused, err := waitForLifecycleEvent(ctx, cycleEnd, signals.cancel, signals.pause, signals.resume, pauseRequested)
		if err != nil {
			return err
		}
		switch {
		case !shouldContinue:
			return cancelMembership(ctx, input, state, charge.StripeSubscriptionID)
		case paused:
			pauseRequested = true
			if err := notifyMembershipState(ctx, input, state, membership.StatePaused, membership.TransitionPause); err != nil {
				return err
			}
		case transition == membership.TransitionResume:
			pauseRequested = false
			if err := notifyMembershipState(ctx, input, state, membership.StateActive, membership.TransitionResume); err != nil {
				return err
			}
			cycleEnd = temporalworkflow.Now(ctx).Add(input.BillingCycle.Duration())
		default:
			renewal, err := renewMembership(ctx, input, state)
			if err != nil {
				return err
			}
			cycleEnd = renewal.CurrentPeriodEnd
		}
	}
}

func renewMembership(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
) (ChargeResponse, error) {
	renewal, err := executeCharge(ctx, input, true)
	if err != nil {
		return ChargeResponse{}, expireMembership(ctx, input, state, err)
	}
	if err := recordMembershipBillingEvent(ctx, input, state, renewal, "renewal"); err != nil {
		return ChargeResponse{}, err
	}
	if err := notifyMembershipState(ctx, input, state, membership.StateActive, membership.TransitionRenew); err != nil {
		return ChargeResponse{}, err
	}
	return renewal, nil
}

func cancelMembership(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
	stripeSubID string,
) error {
	state.FinalState = membership.StateCancelled
	state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateCancelled, membership.TransitionCancel))
	return executeCancellation(ctx, input, stripeSubID)
}

func expireMembership(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
	cause error,
) error {
	state.FinalState = membership.StateExpired
	state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateExpired, membership.TransitionExpire))
	_ = executeNotification(ctx, input, membership.StateExpired, membership.TransitionExpire)
	return fmt.Errorf("renewal charge failed: %w", cause)
}

func notifyMembershipState(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
	next membership.State,
	transition membership.Transition,
) error {
	state.FinalState = next
	state.Notifications = append(state.Notifications, recordNotification(ctx, input, next, transition))
	return executeNotification(ctx, input, next, transition)
}

func recordMembershipBillingEvent(
	ctx temporalworkflow.Context,
	input MembershipLifecycleInput,
	state *MembershipLifecycleResult,
	charge ChargeResponse,
	kind string,
) error {
	event := BillingEventRecord{
		StripeSubscriptionID: charge.StripeSubscriptionID,
		OccurredAt:           charge.CurrentPeriodEnd,
		Kind:                 kind,
	}
	state.BillingEvents = append(state.BillingEvents, event)
	return executeBillingEvent(ctx, BillingEventInput{
		TenantID:             input.TenantID,
		SubscriptionID:       input.SubscriptionID,
		StripeSubscriptionID: charge.StripeSubscriptionID,
		Kind:                 kind,
		OccurredAt:           charge.CurrentPeriodEnd,
	})
}

// waitForLifecycleEvent waits for either the next billing boundary or
// any of the lifecycle signals. Returns:
//   - shouldContinue: false means cancel was received and the workflow
//     should stop.
//   - transition: when non-zero documents which transition was applied
//     (used by the resume path).
//   - paused: true when a pause signal is received and we should stay in
//     paused state on the next iteration.
func waitForLifecycleEvent(
	ctx temporalworkflow.Context,
	deadline time.Time,
	cancelSignal, pauseSignal, resumeSignal temporalworkflow.ReceiveChannel,
	currentlyPaused bool,
) (bool, membership.Transition, bool, error) {
	timer := temporalworkflow.NewTimer(ctx, time.Until(deadline))
	selector := temporalworkflow.NewSelector(ctx)

	canceled := false
	pause := false
	resume := false
	timerFired := false

	selector.AddReceive(cancelSignal, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		var dummy any
		ch.Receive(ctx, &dummy)
		canceled = true
	})
	selector.AddReceive(pauseSignal, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		var dummy any
		ch.Receive(ctx, &dummy)
		pause = true
	})
	selector.AddReceive(resumeSignal, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		var dummy any
		ch.Receive(ctx, &dummy)
		resume = true
	})
	selector.AddFuture(timer, func(temporalworkflow.Future) {
		timerFired = true
	})
	selector.AddReceive(ctx.Done(), func(temporalworkflow.ReceiveChannel, bool) {
		canceled = true
	})
	selector.Select(ctx)

	if canceled {
		return false, membership.TransitionCancel, false, nil
	}
	if pause {
		return true, membership.TransitionPause, true, nil
	}
	if resume {
		if !currentlyPaused {
			// Resume without prior pause is a no-op for replay safety.
			return true, "", false, nil
		}
		return true, membership.TransitionResume, false, nil
	}
	if timerFired {
		return true, membership.TransitionRenew, false, nil
	}
	return true, "", false, errors.New("selector exited without firing")
}

// waitForCancelOrTimer waits for either a cancel signal or the timer to
// fire, whichever comes first. Returns true when the timer fired (no
// cancel) and false when cancel was received.
func waitForCancelOrTimer(ctx temporalworkflow.Context, dur time.Duration, cancelSignal temporalworkflow.ReceiveChannel) (bool, membership.Transition) {
	timer := temporalworkflow.NewTimer(ctx, dur)
	selector := temporalworkflow.NewSelector(ctx)
	timerFired := false
	canceled := false
	selector.AddFuture(timer, func(temporalworkflow.Future) { timerFired = true })
	selector.AddReceive(cancelSignal, func(ch temporalworkflow.ReceiveChannel, _ bool) {
		var dummy any
		ch.Receive(ctx, &dummy)
		canceled = true
	})
	selector.AddReceive(ctx.Done(), func(temporalworkflow.ReceiveChannel, bool) { canceled = true })
	selector.Select(ctx)
	if canceled {
		return false, membership.TransitionCancel
	}
	if timerFired {
		return true, membership.TransitionActivate
	}
	return false, ""
}

func executeCharge(ctx temporalworkflow.Context, input MembershipLifecycleInput, isRenewal bool) (ChargeResponse, error) {
	req := ChargeRequest{
		TenantID:       input.TenantID,
		SubscriptionID: input.SubscriptionID,
		MemberID:       input.MemberID,
		MemberEmail:    input.MemberEmail,
		StripePriceID:  input.StripePriceID,
		BillingCycle:   input.BillingCycle,
		TrialDays:      input.TrialDays,
		IsRenewal:      isRenewal,
	}
	var resp ChargeResponse
	if err := temporalworkflow.ExecuteActivity(ctx, MembershipChargeStripeActivity, req).Get(ctx, &resp); err != nil {
		return ChargeResponse{}, err
	}
	return resp, nil
}

func executeCancellation(ctx temporalworkflow.Context, input MembershipLifecycleInput, stripeSubID string) error {
	if err := executeNotification(ctx, input, membership.StateCancelled, membership.TransitionCancel); err != nil {
		return err
	}
	return executeBillingEvent(ctx, BillingEventInput{
		TenantID:             input.TenantID,
		SubscriptionID:       input.SubscriptionID,
		StripeSubscriptionID: stripeSubID,
		Kind:                 "cancellation",
		OccurredAt:           temporalworkflow.Now(ctx),
	})
}

func executeNotification(ctx temporalworkflow.Context, input MembershipLifecycleInput, state membership.State, transition membership.Transition) error {
	req := NotificationRequest{
		TenantID:       input.TenantID,
		SubscriptionID: input.SubscriptionID,
		MemberID:       input.MemberID,
		MemberEmail:    input.MemberEmail,
		PlanID:         input.PlanID,
		PlanName:       input.PlanName,
		State:          state,
		Transition:     transition,
		OccurredAt:     temporalworkflow.Now(ctx),
	}
	return temporalworkflow.ExecuteActivity(ctx, MembershipSendNotificationActivity, req).Get(ctx, nil)
}

func executeBillingEvent(ctx temporalworkflow.Context, evt BillingEventInput) error {
	return temporalworkflow.ExecuteActivity(ctx, MembershipRecordBillingActivity, evt).Get(ctx, nil)
}

func recordNotification(ctx temporalworkflow.Context, _ MembershipLifecycleInput, state membership.State, transition membership.Transition) NotificationRecord {
	return NotificationRecord{State: state, Transition: transition, OccurredAt: temporalworkflow.Now(ctx)}
}

// ChargeStripe is the activity that wraps the MembershipPaymentGateway.
func (a *MembershipLifecycleActivities) ChargeStripe(ctx context.Context, req ChargeRequest) (ChargeResponse, error) {
	if a.gateway == nil {
		return ChargeResponse{}, errors.New("membership payment gateway is not configured")
	}
	gwResp, err := a.gateway.CreateSubscription(ctx, port.CreateSubscriptionRequest{
		TenantID:       req.TenantID,
		SubscriptionID: req.SubscriptionID,
		MemberID:       req.MemberID,
		MemberEmail:    req.MemberEmail,
		StripePriceID:  req.StripePriceID,
		BillingCycle:   req.BillingCycle,
		TrialDays:      req.TrialDays,
	})
	if err != nil {
		return ChargeResponse{}, err
	}
	return ChargeResponse{
		StripeSubscriptionID: gwResp.StripeSubscriptionID,
		StripeCustomerID:     gwResp.StripeCustomerID,
		CurrentPeriodEnd:     gwResp.CurrentPeriodEnd,
	}, nil
}

// SendNotification is the activity that wraps the
// MembershipNotificationSender port.
func (a *MembershipLifecycleActivities) SendNotification(ctx context.Context, req NotificationRequest) error {
	if a.notifier == nil {
		return nil
	}
	return a.notifier.SendMembershipEvent(ctx, port.MembershipNotificationEvent{
		TenantID:       req.TenantID,
		SubscriptionID: req.SubscriptionID,
		MemberID:       req.MemberID,
		MemberEmail:    req.MemberEmail,
		PlanID:         req.PlanID,
		PlanName:       req.PlanName,
		State:          req.State,
		Transition:     req.Transition,
		OccurredAt:     req.OccurredAt,
	})
}

// RecordBillingEvent is the activity that records a billing event.
func (a *MembershipLifecycleActivities) RecordBillingEvent(ctx context.Context, evt BillingEventInput) error {
	if a.billingLedger == nil {
		return nil
	}
	return a.billingLedger.RecordBillingEvent(ctx, evt)
}
