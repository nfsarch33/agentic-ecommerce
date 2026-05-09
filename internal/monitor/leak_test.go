package monitor

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in
// the v3.4.1 EC-4-5 channel health monitor (or the legacy
// WooCommerce monitor that shares the package) fails the suite.
// Resilience pillar v2.10 gate: every long-lived background loop
// MUST register with lifecycle.Manager + drain via Close.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
