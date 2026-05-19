package subscription_test

import (
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/domain/subscription"
)

func TestCreate_DefaultPending(t *testing.T) {
	t.Parallel()
	mgr := subscription.NewSubscriptionManager()
	sub, err := mgr.Create("user1", "plan-monthly")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if sub.State != subscription.StatePending {
		t.Fatalf("expected pending, got %s", sub.State)
	}
}

func TestFullLifecycle_CreateActivatePauseResumeCancel(t *testing.T) {
	t.Parallel()
	mgr := subscription.NewSubscriptionManager()
	sub, _ := mgr.Create("u1", "monthly")
	sub, err := mgr.Activate(sub.ID)
	if err != nil || sub.State != subscription.StateActive {
		t.Fatalf("Activate: %v / state=%s", err, sub.State)
	}
	sub, err = mgr.Pause(sub.ID)
	if err != nil || sub.State != subscription.StatePaused {
		t.Fatalf("Pause: %v / state=%s", err, sub.State)
	}
	sub, err = mgr.Resume(sub.ID)
	if err != nil || sub.State != subscription.StateActive {
		t.Fatalf("Resume: %v / state=%s", err, sub.State)
	}
	sub, err = mgr.Cancel(sub.ID)
	if err != nil || sub.State != subscription.StateCancelled {
		t.Fatalf("Cancel: %v / state=%s", err, sub.State)
	}
}

func TestInvalidTransition_CancelledToActive(t *testing.T) {
	t.Parallel()
	mgr := subscription.NewSubscriptionManager()
	sub, _ := mgr.Create("u1", "monthly")
	mgr.Activate(sub.ID)
	mgr.Cancel(sub.ID)
	_, err := mgr.Activate(sub.ID)
	if !errors.Is(err, subscription.ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}

func TestBillingCycle_NextDate(t *testing.T) {
	t.Parallel()
	bc := subscription.NewBillingCycle(30 * 24 * time.Hour)
	sub := subscription.Subscription{
		ID:             "s1",
		State:          subscription.StateActive,
		LastBilledAt:   time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
	}
	next := bc.NextBillingDate(sub)
	expected := time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC)
	if !next.Equal(expected) {
		t.Fatalf("expected %v, got %v", expected, next)
	}
}

func TestProration_MidCycle(t *testing.T) {
	t.Parallel()
	// 30-day cycle, 1000 cents/month, cancelled at day 15
	cycle := subscription.NewBillingCycle(30 * 24 * time.Hour)
	start := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 16, 0, 0, 0, 0, time.UTC) // 15 days used
	credit := cycle.ProrationCredit(1000, start, end)
	// 15/30 * 1000 = 500
	if credit < 490 || credit > 510 {
		t.Fatalf("expected ~500 credit, got %d", credit)
	}
}

func TestGracePeriod(t *testing.T) {
	t.Parallel()
	mgr := subscription.NewSubscriptionManager()
	sub, _ := mgr.Create("u1", "monthly")
	mgr.Activate(sub.ID)
	mgr.Cancel(sub.ID)

	sub, _ = mgr.Get(sub.ID)
	graceEnd := sub.CancelledAt.Add(7 * 24 * time.Hour)
	if graceEnd.Before(sub.CancelledAt) {
		t.Fatal("grace period should extend beyond cancellation")
	}
}

func TestCreate_InvalidPlan(t *testing.T) {
	t.Parallel()
	mgr := subscription.NewSubscriptionManager()
	_, err := mgr.Create("u1", "")
	if err == nil {
		t.Fatal("expected error for empty plan")
	}
}
