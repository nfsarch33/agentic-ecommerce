package workflow

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
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
	activityOptions := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, activityOptions)

	state := MembershipLifecycleResult{
		TenantID:       input.TenantID,
		SubscriptionID: input.SubscriptionID,
		FinalState:     membership.StateTrial,
	}
	if err := temporalworkflow.SetQueryHandler(ctx, MembershipStatusQuery, func() (MembershipLifecycleResult, error) {
		return state, nil
	}); err != nil {
		return state, err
	}

	cancelSignal := temporalworkflow.GetSignalChannel(ctx, MembershipCancelSignal)
	pauseSignal := temporalworkflow.GetSignalChannel(ctx, MembershipPauseSignal)
	resumeSignal := temporalworkflow.GetSignalChannel(ctx, MembershipResumeSignal)

	// Initial charge through the gateway. Even a trial subscription
	// records a deterministic stripe id so the rest of the workflow can
	// emit billing events keyed off it.
	charge, err := executeCharge(ctx, input, false)
	if err != nil {
		return state, err
	}
	state.StripeSubscriptionID = charge.StripeSubscriptionID
	state.BillingEvents = append(state.BillingEvents, BillingEventRecord{
		StripeSubscriptionID: charge.StripeSubscriptionID,
		OccurredAt:           charge.CurrentPeriodEnd,
		Kind:                 "initial",
	})
	state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateTrial, membership.TransitionActivate))
	if err := executeNotification(ctx, input, membership.StateTrial, membership.TransitionActivate); err != nil {
		return state, err
	}
	if err := executeBillingEvent(ctx, BillingEventInput{
		TenantID:             input.TenantID,
		SubscriptionID:       input.SubscriptionID,
		StripeSubscriptionID: charge.StripeSubscriptionID,
		Kind:                 "initial",
		OccurredAt:           charge.CurrentPeriodEnd,
	}); err != nil {
		return state, err
	}

	// Trial timer (only if requested).
	if input.TrialDays > 0 {
		trial, action := waitForCancelOrTimer(ctx, time.Duration(input.TrialDays)*24*time.Hour, cancelSignal)
		if !trial {
			state.FinalState = membership.StateCancelled
			state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateCancelled, membership.TransitionCancel))
			if err := executeCancellation(ctx, input, charge.StripeSubscriptionID); err != nil {
				return state, err
			}
			_ = action
			return state, nil
		}
	}

	state.FinalState = membership.StateActive
	state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateActive, membership.TransitionActivate))
	if err := executeNotification(ctx, input, membership.StateActive, membership.TransitionActivate); err != nil {
		return state, err
	}

	// Renewal loop: one cycle per billing period until cancel/expire.
	pauseRequested := false
	cycleEnd := temporalworkflow.Now(ctx).Add(input.BillingCycle.Duration())
	for {
		shouldContinue, transition, paused, err := waitForLifecycleEvent(ctx, cycleEnd, cancelSignal, pauseSignal, resumeSignal, pauseRequested)
		if err != nil {
			return state, err
		}
		if !shouldContinue {
			state.FinalState = membership.StateCancelled
			state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateCancelled, membership.TransitionCancel))
			if err := executeCancellation(ctx, input, charge.StripeSubscriptionID); err != nil {
				return state, err
			}
			return state, nil
		}
		if paused {
			pauseRequested = true
			state.FinalState = membership.StatePaused
			state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StatePaused, membership.TransitionPause))
			if err := executeNotification(ctx, input, membership.StatePaused, membership.TransitionPause); err != nil {
				return state, err
			}
			continue
		}
		if transition == membership.TransitionResume {
			pauseRequested = false
			state.FinalState = membership.StateActive
			state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateActive, membership.TransitionResume))
			if err := executeNotification(ctx, input, membership.StateActive, membership.TransitionResume); err != nil {
				return state, err
			}
			cycleEnd = temporalworkflow.Now(ctx).Add(input.BillingCycle.Duration())
			continue
		}

		// Renewal: charge + advance the period window.
		renewal, err := executeCharge(ctx, input, true)
		if err != nil {
			// On payment failure mid-renewal, expire the subscription.
			state.FinalState = membership.StateExpired
			state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateExpired, membership.TransitionExpire))
			_ = executeNotification(ctx, input, membership.StateExpired, membership.TransitionExpire)
			return state, fmt.Errorf("renewal charge failed: %w", err)
		}
		state.BillingEvents = append(state.BillingEvents, BillingEventRecord{
			StripeSubscriptionID: renewal.StripeSubscriptionID,
			OccurredAt:           renewal.CurrentPeriodEnd,
			Kind:                 "renewal",
		})
		if err := executeBillingEvent(ctx, BillingEventInput{
			TenantID:             input.TenantID,
			SubscriptionID:       input.SubscriptionID,
			StripeSubscriptionID: renewal.StripeSubscriptionID,
			Kind:                 "renewal",
			OccurredAt:           renewal.CurrentPeriodEnd,
		}); err != nil {
			return state, err
		}
		state.Notifications = append(state.Notifications, recordNotification(ctx, input, membership.StateActive, membership.TransitionRenew))
		if err := executeNotification(ctx, input, membership.StateActive, membership.TransitionRenew); err != nil {
			return state, err
		}
		cycleEnd = renewal.CurrentPeriodEnd
	}
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
