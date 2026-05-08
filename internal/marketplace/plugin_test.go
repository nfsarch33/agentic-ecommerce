package marketplace

import "testing"

func TestEventNamesCopiesSlice(t *testing.T) {
	t.Parallel()
	m := Manifest{
		EventSubscriptions: []EventName{"order.placed", "order.cancelled"},
	}
	got := EventNames(m)
	if len(got) != 2 {
		t.Fatalf("EventNames should return 2 items, got %d", len(got))
	}
	got[0] = "mutated"
	if m.EventSubscriptions[0] == "mutated" {
		t.Fatalf("EventNames must defensive-copy")
	}
}

func TestInstallationIsActive(t *testing.T) {
	t.Parallel()
	if !((Installation{State: StateActive}).IsActive()) {
		t.Fatalf("active installation should report IsActive")
	}
	if (Installation{State: StateInstalled}).IsActive() {
		t.Fatalf("installed installation should not report IsActive")
	}
}
