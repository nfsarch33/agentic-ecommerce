package notification

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type stubPublisher struct {
	published []eventbus.Event
	err       error
}

func (s *stubPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	if s.err != nil {
		return s.err
	}
	s.published = append(s.published, evt)
	return nil
}
func (s *stubPublisher) Close() error { return nil }

func TestBusSenderTransitionMapping(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		transition membership.Transition
		state      membership.State
		wantType   eventbus.EventType
		wantSkip   bool
	}{
		{"trial activate -> created", membership.TransitionActivate, membership.StateTrial, eventbus.MembershipCreated, false},
		{"active activate -> created", membership.TransitionActivate, membership.StateActive, eventbus.MembershipCreated, false},
		{"renew -> renewed", membership.TransitionRenew, membership.StateActive, eventbus.MembershipRenewed, false},
		{"cancel -> cancelled", membership.TransitionCancel, membership.StateCancelled, eventbus.MembershipCancelled, false},
		{"pause -> paused", membership.TransitionPause, membership.StatePaused, eventbus.MembershipPaused, false},
		{"resume -> resumed", membership.TransitionResume, membership.StateActive, eventbus.MembershipResumed, false},
		{"expire -> dropped", membership.TransitionExpire, membership.StateExpired, "", true},
	}
	memberID := uuid.New()
	subID := uuid.New()
	planID := uuid.New()
	occurred := time.Date(2026, 5, 8, 17, 30, 0, 0, time.UTC)
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			pub := &stubPublisher{}
			sender := NewBusSender(pub, "workflow.test")
			err := sender.SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{
				TenantID:       "tenant-a",
				SubscriptionID: subID,
				MemberID:       memberID,
				MemberEmail:    "alice@example.com",
				PlanID:         planID,
				PlanName:       "Pro",
				State:          tc.state,
				Transition:     tc.transition,
				OccurredAt:     occurred,
			})
			if err != nil {
				t.Fatalf("send: %v", err)
			}
			if tc.wantSkip {
				if len(pub.published) != 0 {
					t.Fatalf("expected no publish, got %d", len(pub.published))
				}
				return
			}
			if len(pub.published) != 1 {
				t.Fatalf("expected 1 publish, got %d", len(pub.published))
			}
			got := pub.published[0]
			if got.Type != tc.wantType {
				t.Fatalf("event type = %v, want %v", got.Type, tc.wantType)
			}
			if got.TenantID != "tenant-a" {
				t.Fatalf("tenant = %q", got.TenantID)
			}
			if got.Source != "workflow.test" {
				t.Fatalf("source = %q", got.Source)
			}
			if got.Payload["plan_name"] != "Pro" {
				t.Fatalf("plan_name = %v", got.Payload["plan_name"])
			}
		})
	}
}

func TestBusSenderNilGuards(t *testing.T) {
	t.Parallel()
	var sender *BusSender
	if err := sender.SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{}); err != nil {
		t.Fatalf("nil receiver should be no-op, got %v", err)
	}
	if err := NewBusSender(nil, "").SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{}); err != nil {
		t.Fatalf("nil publisher should be no-op, got %v", err)
	}
}

func TestBusSenderPropagatesPublishError(t *testing.T) {
	t.Parallel()
	want := errors.New("bus down")
	pub := &stubPublisher{err: want}
	sender := NewBusSender(pub, "")
	err := sender.SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{
		TenantID:       "t-1",
		SubscriptionID: uuid.New(),
		MemberID:       uuid.New(),
		PlanID:         uuid.New(),
		State:          membership.StateActive,
		Transition:     membership.TransitionRenew,
		OccurredAt:     time.Now().UTC(),
	})
	if err == nil || !errors.Is(err, want) {
		t.Fatalf("expected publisher error to propagate, got %v", err)
	}
}

type stubSender struct {
	calls int
	err   error
}

func (s *stubSender) SendMembershipEvent(_ context.Context, _ port.MembershipNotificationEvent) error {
	s.calls++
	return s.err
}

func TestMultiSenderFanOut(t *testing.T) {
	t.Parallel()
	a := &stubSender{}
	b := &stubSender{}
	multi := NewMultiSender(a, nil, b)
	if err := multi.SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{}); err != nil {
		t.Fatalf("send: %v", err)
	}
	if a.calls != 1 || b.calls != 1 {
		t.Fatalf("expected fan-out, got a=%d b=%d", a.calls, b.calls)
	}
}

func TestMultiSenderJoinsErrors(t *testing.T) {
	t.Parallel()
	errA := errors.New("a failed")
	errB := errors.New("b failed")
	multi := NewMultiSender(&stubSender{err: errA}, &stubSender{}, &stubSender{err: errB})
	err := multi.SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{})
	if err == nil {
		t.Fatalf("expected joined error")
	}
	if !errors.Is(err, errA) || !errors.Is(err, errB) {
		t.Fatalf("expected both errors via errors.Is, got %v", err)
	}
}

func TestMultiSenderEmpty(t *testing.T) {
	t.Parallel()
	if err := NewMultiSender().SendMembershipEvent(context.Background(), port.MembershipNotificationEvent{}); err != nil {
		t.Fatalf("empty multi should be no-op, got %v", err)
	}
}
