package alert

import "testing"

func TestAlertTypeConstants(t *testing.T) {
	t.Parallel()
	types := []AlertType{
		TypeLargeRefund, TypeLargeDropship, TypePriceChange,
		TypeCAPTCHADetected, TypeOmniUnavailable, TypeRateLimitDrain,
		TypeChannelStatusFail, TypeLargeMargin,
	}
	seen := map[AlertType]bool{}
	for _, at := range types {
		if at == "" {
			t.Fatal("empty alert type constant")
		}
		if seen[at] {
			t.Fatalf("duplicate alert type: %s", at)
		}
		seen[at] = true
	}
	if len(seen) != 8 {
		t.Fatalf("expected 8 alert types, got %d", len(seen))
	}
}

func TestAlertStatusConstants(t *testing.T) {
	t.Parallel()
	statuses := []AlertStatus{StatusPending, StatusAcknowledged, StatusResolved, StatusExpired}
	seen := map[AlertStatus]bool{}
	for _, s := range statuses {
		if s == "" {
			t.Fatal("empty alert status constant")
		}
		if seen[s] {
			t.Fatalf("duplicate alert status: %s", s)
		}
		seen[s] = true
	}
}

func TestAlertSeverityConstants(t *testing.T) {
	t.Parallel()
	sevs := []AlertSeverity{SeverityInfo, SeverityWarning, SeverityCritical}
	seen := map[AlertSeverity]bool{}
	for _, s := range sevs {
		if s == "" {
			t.Fatal("empty severity constant")
		}
		if seen[s] {
			t.Fatalf("duplicate severity: %s", s)
		}
		seen[s] = true
	}
}
