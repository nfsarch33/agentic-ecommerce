package carrier

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in the
// v3.8.0 EC-7-3 carrier adapter package fails the suite. Resilience
// pillar v2.10 gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
