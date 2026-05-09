package billing

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v2.5.0 billing package (Stripe webhook + service + Subscription
// state) OR the v3.5.0 EC-6-2 platform-fee + FX provider code path
// fails the suite. Resilience pillar v2.10 gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
