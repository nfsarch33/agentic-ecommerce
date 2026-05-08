package registration

import (
	"context"
	"testing"
	"time"
)

func TestRecorderRecordsRequestedAndVerified(t *testing.T) {
	t.Parallel()
	r := NewRecorder()
	r.now = func() time.Time { return time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC) }
	req, _ := NewRequest(SubmitInput{Email: "alice@example.com", SlugRequested: "tenant-a"}, time.Hour)
	if err := r.NotifyRegistrationRequested(context.Background(), req, "tok"); err != nil {
		t.Fatalf("requested: %v", err)
	}
	if err := r.NotifyRegistrationVerified(context.Background(), req); err != nil {
		t.Fatalf("verified: %v", err)
	}
	events := r.Events()
	if len(events) != 2 {
		t.Fatalf("len events = %d", len(events))
	}
	if events[0].Kind != NotificationRequested || events[0].Token != "tok" {
		t.Fatalf("first event = %+v", events[0])
	}
	if events[1].Kind != NotificationVerified {
		t.Fatalf("second event kind = %s", events[1].Kind)
	}
}
