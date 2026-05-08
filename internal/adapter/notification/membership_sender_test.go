package notification_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/notification"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

func TestMembershipNotificationRecorderRecords(t *testing.T) {
	t.Parallel()
	rec := notification.NewMembershipNotificationRecorder()
	evt := port.MembershipNotificationEvent{
		TenantID:       "tenant-a",
		SubscriptionID: uuid.New(),
		MemberID:       uuid.New(),
		MemberEmail:    "alice@example.com",
		PlanID:         uuid.New(),
		PlanName:       "Gold",
		State:          membership.StateActive,
		Transition:     membership.TransitionActivate,
		OccurredAt:     time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC),
	}
	if err := rec.SendMembershipEvent(context.Background(), evt); err != nil {
		t.Fatalf("Send: %v", err)
	}
	got := rec.Events()
	if len(got) != 1 || got[0].State != membership.StateActive {
		t.Fatalf("events = %+v", got)
	}

	// Recorded slice is defensive: mutate caller copy and observe stable internal state.
	got[0].State = membership.StateExpired
	again := rec.Events()
	if again[0].State != membership.StateActive {
		t.Fatal("Events() returned aliased slice")
	}
}
