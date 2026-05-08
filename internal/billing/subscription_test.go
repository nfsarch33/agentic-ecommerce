package billing

import (
	"errors"
	"testing"
	"time"
)

func TestNewSubscriptionDefaults(t *testing.T) {
	t.Parallel()
	in := NewSubscriptionInput{
		ID:       "sub_1",
		TenantID: "tenant-a",
		PlanID:   "free",
		Now:      time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
	got, err := NewSubscription(in)
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	if got.State != StateTrialing {
		t.Fatalf("default state = %s, want trialing", got.State)
	}
	if !got.CreatedAt.Equal(in.Now) {
		t.Fatalf("created_at = %v, want %v", got.CreatedAt, in.Now)
	}
}

func TestNewSubscriptionRequired(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		mutate  func(*NewSubscriptionInput)
		wantErr error
	}{
		{"missing id", func(in *NewSubscriptionInput) { in.ID = "" }, ErrSubscriptionNotFound},
		{"missing tenant", func(in *NewSubscriptionInput) { in.TenantID = "" }, ErrTenantRequired},
		{"missing plan", func(in *NewSubscriptionInput) { in.PlanID = "" }, ErrPlanNotFound},
		{"bad state", func(in *NewSubscriptionInput) { in.State = "made-up" }, ErrInvalidState},
	}
	base := NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			in := base
			tc.mutate(&in)
			_, err := NewSubscription(in)
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("expected %v, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestSubscriptionApply(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	sub, err := NewSubscription(NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free", Now: now})
	if err != nil {
		t.Fatalf("NewSubscription: %v", err)
	}
	activated, err := sub.Apply(TransitionActivate, now.Add(time.Minute))
	if err != nil {
		t.Fatalf("Apply activate: %v", err)
	}
	if activated.State != StateActive {
		t.Fatalf("state after activate = %s", activated.State)
	}
	if !activated.UpdatedAt.After(sub.UpdatedAt) {
		t.Fatalf("updated_at not advanced: before=%v after=%v", sub.UpdatedAt, activated.UpdatedAt)
	}
}

func TestSubscriptionApplyIllegal(t *testing.T) {
	t.Parallel()
	sub, _ := NewSubscription(NewSubscriptionInput{ID: "sub_1", TenantID: "tenant-a", PlanID: "free"})
	if _, err := sub.Apply(TransitionResume, time.Time{}); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition, got %v", err)
	}
}
