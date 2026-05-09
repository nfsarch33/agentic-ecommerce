package china

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in
// the China adapter package fails the suite. v3.1.0 EC-1-1/EC-1-2
// resilience pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
