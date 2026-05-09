package customerservice

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.6.0 EC-8-1 + EC-8-2 customer service package fails the suite.
// Resilience pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
