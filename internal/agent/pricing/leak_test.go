package pricing

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in
// the v3.5.0 EC-6-3 dynamic pricing agent fails the suite.
// Resilience pillar v2.10 gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
