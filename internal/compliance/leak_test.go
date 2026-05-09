package compliance

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// compliance package fails the suite. v3.1.0 EC-1-4 resilience pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
