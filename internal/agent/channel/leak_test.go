package channel

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in
// the v3.3.0 EC-3-2 channel package fails the suite. Resilience
// pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
