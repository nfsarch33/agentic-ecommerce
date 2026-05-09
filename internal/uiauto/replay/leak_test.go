package replay

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.7.0 EC-10-5 replay harness package fails the suite.
// Resilience pillar gate (12-sprint streak target).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
