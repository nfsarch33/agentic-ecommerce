package membership

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrMemberRequired       = errors.New("subscription member id is required")
	ErrSubscriptionPlanFK   = errors.New("subscription plan id is required")
	ErrTrialEndsBeforeStart = errors.New("subscription trial ends before it starts")
	ErrPlanTenantMismatch   = errors.New("subscription plan tenant does not match member tenant")
)

// SubscriptionInput is the constructor payload for a Subscription.
type SubscriptionInput struct {
	TenantID  string
	MemberID  uuid.UUID
	PlanID    uuid.UUID
	TrialDays int
	Now       time.Time
}

// SubscriptionRecord is the repository hydration shape.
type SubscriptionRecord struct {
	ID                   uuid.UUID
	TenantID             string
	MemberID             uuid.UUID
	PlanID               uuid.UUID
	State                State
	CurrentPeriodStart   time.Time
	CurrentPeriodEnd     time.Time
	TrialEndsAt          time.Time
	StripeSubscriptionID string
	CancelledAt          *time.Time
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// Subscription is the lifecycle aggregate. Mutations are forced through
// Apply(Transition, ...) so the state machine is never bypassed.
type Subscription struct {
	id                   uuid.UUID
	tenantID             string
	memberID             uuid.UUID
	planID               uuid.UUID
	state                State
	currentPeriodStart   time.Time
	currentPeriodEnd     time.Time
	trialEndsAt          time.Time
	stripeSubscriptionID string
	cancelledAt          *time.Time
	createdAt            time.Time
	updatedAt            time.Time
}

// NewSubscription creates a Subscription in the trial state. The caller is
// expected to have validated plan+member tenant alignment upstream; this
// function double-checks tenant id non-empty and trial window sanity.
func NewSubscription(input SubscriptionInput, plan MembershipPlan) (Subscription, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return Subscription{}, ErrTenantRequired
	}
	if input.MemberID == uuid.Nil {
		return Subscription{}, ErrMemberRequired
	}
	if input.PlanID == uuid.Nil {
		return Subscription{}, ErrSubscriptionPlanFK
	}
	if plan.TenantID() != tenantID {
		return Subscription{}, ErrPlanTenantMismatch
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	trialDays := input.TrialDays
	if trialDays < 0 {
		trialDays = 0
	}
	trialEnds := now.Add(time.Duration(trialDays) * 24 * time.Hour)
	if trialEnds.Before(now) {
		return Subscription{}, ErrTrialEndsBeforeStart
	}

	periodEnd := trialEnds
	if trialDays == 0 {
		// No trial: first billing period is one billing-cycle long, but the
		// state still starts as trial so the workflow can perform the first
		// charge through the explicit Activate transition.
		periodEnd = now.Add(plan.BillingCycle().Duration())
	}

	return Subscription{
		id:                 uuid.New(),
		tenantID:           tenantID,
		memberID:           input.MemberID,
		planID:             input.PlanID,
		state:              StateTrial,
		currentPeriodStart: now,
		currentPeriodEnd:   periodEnd,
		trialEndsAt:        trialEnds,
		createdAt:          now,
		updatedAt:          now,
	}, nil
}

// ReconstructSubscription hydrates a Subscription from a repository record.
func ReconstructSubscription(rec SubscriptionRecord) Subscription {
	return Subscription{
		id:                   rec.ID,
		tenantID:             rec.TenantID,
		memberID:             rec.MemberID,
		planID:               rec.PlanID,
		state:                rec.State,
		currentPeriodStart:   rec.CurrentPeriodStart,
		currentPeriodEnd:     rec.CurrentPeriodEnd,
		trialEndsAt:          rec.TrialEndsAt,
		stripeSubscriptionID: rec.StripeSubscriptionID,
		cancelledAt:          rec.CancelledAt,
		createdAt:            rec.CreatedAt,
		updatedAt:            rec.UpdatedAt,
	}
}

// Apply moves the Subscription through the state machine. The caller must
// inject `now` so workflow code stays deterministic; production callers
// pass time.Now().UTC().
func (s *Subscription) Apply(t Transition, plan MembershipPlan, now time.Time) error {
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	target, err := nextState(s.state, t)
	if err != nil {
		return err
	}
	previous := s.state
	s.state = target
	s.updatedAt = now

	switch t {
	case TransitionActivate:
		s.currentPeriodStart = now
		s.currentPeriodEnd = now.Add(plan.BillingCycle().Duration())
	case TransitionRenew:
		s.currentPeriodStart = s.currentPeriodEnd
		s.currentPeriodEnd = s.currentPeriodStart.Add(plan.BillingCycle().Duration())
	case TransitionPause:
		// No period mutation; the workflow records the pause time via
		// s.updatedAt and resumes from the original boundary.
	case TransitionResume:
		// Resume from now: shift the period window forward so the customer
		// is not double-billed for the paused interval.
		periodLen := s.currentPeriodEnd.Sub(s.currentPeriodStart)
		s.currentPeriodStart = now
		s.currentPeriodEnd = now.Add(periodLen)
	case TransitionCancel:
		cancelled := now
		s.cancelledAt = &cancelled
	case TransitionExpire:
		// Expiry leaves the period window in place for audit, no further
		// transitions allowed (terminal state).
	}
	_ = previous
	return nil
}

// LinkStripeSubscription attaches a Stripe subscription identifier (set
// from the PaymentGateway adapter). It does not change state.
func (s *Subscription) LinkStripeSubscription(stripeSubscriptionID string) {
	s.stripeSubscriptionID = strings.TrimSpace(stripeSubscriptionID)
	s.updatedAt = time.Now().UTC()
}

func (s Subscription) ID() uuid.UUID                 { return s.id }
func (s Subscription) TenantID() string              { return s.tenantID }
func (s Subscription) MemberID() uuid.UUID           { return s.memberID }
func (s Subscription) PlanID() uuid.UUID             { return s.planID }
func (s Subscription) State() State                  { return s.state }
func (s Subscription) CurrentPeriodStart() time.Time { return s.currentPeriodStart }
func (s Subscription) CurrentPeriodEnd() time.Time   { return s.currentPeriodEnd }
func (s Subscription) TrialEndsAt() time.Time        { return s.trialEndsAt }
func (s Subscription) StripeSubscriptionID() string  { return s.stripeSubscriptionID }
func (s Subscription) CancelledAt() *time.Time {
	if s.cancelledAt == nil {
		return nil
	}
	cancelled := *s.cancelledAt
	return &cancelled
}
func (s Subscription) CreatedAt() time.Time { return s.createdAt }
func (s Subscription) UpdatedAt() time.Time { return s.updatedAt }
