package waitlist_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/waitlist"
)

func TestWaitlist_SubscribeAddsToQueue(t *testing.T) {
	t.Parallel()
	a := waitlist.NewAlerter()
	a.Subscribe("U1", "P1")
	notified, _ := a.NotifyInStock("P1")
	if len(notified) != 1 || notified[0] != "U1" {
		t.Fatalf("expected [U1], got %v", notified)
	}
}

func TestWaitlist_UnsubscribeRemoves(t *testing.T) {
	t.Parallel()
	a := waitlist.NewAlerter()
	a.Subscribe("U1", "P1")
	a.Subscribe("U2", "P1")
	a.Unsubscribe("U1", "P1")
	notified, _ := a.NotifyInStock("P1")
	if len(notified) != 1 || notified[0] != "U2" {
		t.Fatalf("expected [U2], got %v", notified)
	}
}

func TestWaitlist_NotifyReturnsFIFOOrder(t *testing.T) {
	t.Parallel()
	a := waitlist.NewAlerter()
	a.Subscribe("U1", "P2")
	a.Subscribe("U2", "P2")
	a.Subscribe("U3", "P2")
	notified, _ := a.NotifyInStock("P2")
	if len(notified) != 3 {
		t.Fatalf("expected 3, got %d", len(notified))
	}
	if notified[0] != "U1" {
		t.Fatalf("expected U1 first (FIFO), got %s", notified[0])
	}
}

func TestWaitlist_NotifyClearsQueue(t *testing.T) {
	t.Parallel()
	a := waitlist.NewAlerter()
	a.Subscribe("U1", "P3")
	a.NotifyInStock("P3")
	notified, _ := a.NotifyInStock("P3")
	if len(notified) != 0 {
		t.Fatalf("expected empty after clear, got %v", notified)
	}
}

func TestWaitlist_DuplicateSubscribeIdempotent(t *testing.T) {
	t.Parallel()
	a := waitlist.NewAlerter()
	a.Subscribe("U1", "P4")
	a.Subscribe("U1", "P4")
	notified, _ := a.NotifyInStock("P4")
	if len(notified) != 1 {
		t.Fatalf("expected 1 (dedup), got %d", len(notified))
	}
}

func TestWaitlist_UnsubscribeNonExistentNoError(t *testing.T) {
	t.Parallel()
	a := waitlist.NewAlerter()
	if err := a.Unsubscribe("NOBODY", "P5"); err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
}
