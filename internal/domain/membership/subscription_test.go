package membership

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
)

var fixedNow = time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)

func newTestPlan(t *testing.T, tenant string) MembershipPlan {
	t.Helper()
	plan, err := NewMembershipPlan(PlanInput{
		TenantID:     tenant,
		Name:         "Gold",
		BillingCycle: BillingCycleMonthly,
		Price:        mustMoney(t, 1995, "AUD"),
	})
	if err != nil {
		t.Fatalf("plan: %v", err)
	}
	return plan
}

func TestNewSubscriptionRequiresInvariants(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(t, "tenant-a")

	cases := []struct {
		name    string
		input   SubscriptionInput
		plan    MembershipPlan
		wantErr error
	}{
		{
			name: "missing tenant",
			input: SubscriptionInput{
				MemberID:  uuid.New(),
				PlanID:    plan.ID(),
				TrialDays: 7,
				Now:       fixedNow,
			},
			plan:    plan,
			wantErr: ErrTenantRequired,
		},
		{
			name: "missing member id",
			input: SubscriptionInput{
				TenantID:  "tenant-a",
				PlanID:    plan.ID(),
				TrialDays: 7,
				Now:       fixedNow,
			},
			plan:    plan,
			wantErr: ErrMemberRequired,
		},
		{
			name: "missing plan id",
			input: SubscriptionInput{
				TenantID:  "tenant-a",
				MemberID:  uuid.New(),
				TrialDays: 7,
				Now:       fixedNow,
			},
			plan:    plan,
			wantErr: ErrSubscriptionPlanFK,
		},
		{
			name: "plan tenant mismatch",
			input: SubscriptionInput{
				TenantID:  "tenant-b",
				MemberID:  uuid.New(),
				PlanID:    plan.ID(),
				TrialDays: 7,
				Now:       fixedNow,
			},
			plan:    plan,
			wantErr: ErrPlanTenantMismatch,
		},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewSubscription(tc.input, tc.plan)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("err = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

func TestNewSubscriptionTrialAndImmediateBilling(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(t, "tenant-a")
	memberID := uuid.New()

	withTrial, err := NewSubscription(SubscriptionInput{
		TenantID:  "tenant-a",
		MemberID:  memberID,
		PlanID:    plan.ID(),
		TrialDays: 14,
		Now:       fixedNow,
	}, plan)
	if err != nil {
		t.Fatalf("with trial: %v", err)
	}
	if withTrial.State() != StateTrial {
		t.Fatalf("state = %s, want trial", withTrial.State())
	}
	if !withTrial.TrialEndsAt().Equal(fixedNow.AddDate(0, 0, 14)) {
		t.Fatalf("trial ends = %s", withTrial.TrialEndsAt())
	}

	noTrial, err := NewSubscription(SubscriptionInput{
		TenantID:  "tenant-a",
		MemberID:  memberID,
		PlanID:    plan.ID(),
		TrialDays: 0,
		Now:       fixedNow,
	}, plan)
	if err != nil {
		t.Fatalf("no trial: %v", err)
	}
	if !noTrial.CurrentPeriodEnd().Equal(fixedNow.Add(plan.BillingCycle().Duration())) {
		t.Fatalf("current period end = %s", noTrial.CurrentPeriodEnd())
	}
}

// TestSubscriptionApplyAllValidTransitions exhaustively walks every
// (state, transition) cell in the table to confirm Apply mutates the
// aggregate exactly as the table-driven contract promises.
func TestSubscriptionApplyAllValidTransitions(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(t, "tenant-a")
	memberID := uuid.New()

	cases := []struct {
		name    string
		seed    State
		via     Transition
		want    State
		bumpsTs bool
	}{
		{name: "trial->activate", seed: StateTrial, via: TransitionActivate, want: StateActive, bumpsTs: true},
		{name: "trial->cancel", seed: StateTrial, via: TransitionCancel, want: StateCancelled, bumpsTs: true},
		{name: "trial->expire", seed: StateTrial, via: TransitionExpire, want: StateExpired, bumpsTs: true},
		{name: "active->pause", seed: StateActive, via: TransitionPause, want: StatePaused, bumpsTs: true},
		{name: "active->renew", seed: StateActive, via: TransitionRenew, want: StateActive, bumpsTs: true},
		{name: "active->cancel", seed: StateActive, via: TransitionCancel, want: StateCancelled, bumpsTs: true},
		{name: "active->expire", seed: StateActive, via: TransitionExpire, want: StateExpired, bumpsTs: true},
		{name: "paused->resume", seed: StatePaused, via: TransitionResume, want: StateActive, bumpsTs: true},
		{name: "paused->cancel", seed: StatePaused, via: TransitionCancel, want: StateCancelled, bumpsTs: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sub, err := NewSubscription(SubscriptionInput{
				TenantID:  "tenant-a",
				MemberID:  memberID,
				PlanID:    plan.ID(),
				TrialDays: 7,
				Now:       fixedNow,
			}, plan)
			if err != nil {
				t.Fatalf("seed sub: %v", err)
			}
			// Force the seed state so we test from any starting point.
			sub.state = tc.seed

			beforeUpdated := sub.UpdatedAt()
			now := fixedNow.Add(time.Hour)
			if err := sub.Apply(tc.via, plan, now); err != nil {
				t.Fatalf("Apply: %v", err)
			}
			if sub.State() != tc.want {
				t.Fatalf("state = %s, want %s", sub.State(), tc.want)
			}
			if tc.bumpsTs && !sub.UpdatedAt().After(beforeUpdated) {
				t.Fatalf("updatedAt = %s, want > %s", sub.UpdatedAt(), beforeUpdated)
			}
			if tc.via == TransitionCancel && sub.CancelledAt() == nil {
				t.Fatal("CancelledAt = nil after cancel")
			}
			if tc.via == TransitionRenew {
				if !sub.CurrentPeriodEnd().Equal(sub.CurrentPeriodStart().Add(plan.BillingCycle().Duration())) {
					t.Fatalf("renew window = [%s, %s], expected one cycle", sub.CurrentPeriodStart(), sub.CurrentPeriodEnd())
				}
			}
		})
	}
}

// TestSubscriptionApplyInvalidTransitionsReturnTypedError confirms every
// (state, transition) outside the table returns ErrInvalidTransition.
func TestSubscriptionApplyInvalidTransitionsReturnTypedError(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(t, "tenant-a")
	memberID := uuid.New()

	allStates := []State{StateTrial, StateActive, StatePaused, StateCancelled, StateExpired}
	allTransitions := []Transition{
		TransitionActivate, TransitionPause, TransitionResume,
		TransitionCancel, TransitionExpire, TransitionRenew,
	}

	for _, from := range allStates {
		for _, via := range allTransitions {
			from, via := from, via
			if _, ok := transitionTable[from][via]; ok {
				continue
			}
			t.Run(string(from)+"-"+string(via), func(t *testing.T) {
				t.Parallel()
				sub, err := NewSubscription(SubscriptionInput{
					TenantID:  "tenant-a",
					MemberID:  memberID,
					PlanID:    plan.ID(),
					TrialDays: 0,
					Now:       fixedNow,
				}, plan)
				if err != nil {
					t.Fatalf("seed: %v", err)
				}
				sub.state = from
				if err := sub.Apply(via, plan, fixedNow); !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("Apply(%s, %s) err = %v, want ErrInvalidTransition", from, via, err)
				}
				if sub.State() != from {
					t.Fatalf("invalid transition mutated state to %s", sub.State())
				}
			})
		}
	}
}

func TestSubscriptionLinkStripeSubscription(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(t, "tenant-a")
	sub, err := NewSubscription(SubscriptionInput{
		TenantID:  "tenant-a",
		MemberID:  uuid.New(),
		PlanID:    plan.ID(),
		TrialDays: 0,
		Now:       fixedNow,
	}, plan)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	sub.LinkStripeSubscription("  sub_abc  ")
	if sub.StripeSubscriptionID() != "sub_abc" {
		t.Fatalf("stripe id = %q", sub.StripeSubscriptionID())
	}
}

func TestReconstructSubscriptionRoundtrip(t *testing.T) {
	t.Parallel()

	plan := newTestPlan(t, "tenant-a")
	original, err := NewSubscription(SubscriptionInput{
		TenantID:  "tenant-a",
		MemberID:  uuid.New(),
		PlanID:    plan.ID(),
		TrialDays: 14,
		Now:       fixedNow,
	}, plan)
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := SubscriptionRecord{
		ID:                   original.ID(),
		TenantID:             original.TenantID(),
		MemberID:             original.MemberID(),
		PlanID:               original.PlanID(),
		State:                original.State(),
		CurrentPeriodStart:   original.CurrentPeriodStart(),
		CurrentPeriodEnd:     original.CurrentPeriodEnd(),
		TrialEndsAt:          original.TrialEndsAt(),
		StripeSubscriptionID: original.StripeSubscriptionID(),
		CreatedAt:            original.CreatedAt(),
		UpdatedAt:            original.UpdatedAt(),
	}
	rebuilt := ReconstructSubscription(rec)
	if rebuilt.ID() != original.ID() || rebuilt.State() != original.State() || rebuilt.MemberID() != original.MemberID() {
		t.Fatalf("reconstruct mismatch: %+v vs %+v", rebuilt, original)
	}
}
