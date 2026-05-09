package handler

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.6.0 EC-9-1 + EC-9-2 handler package fails the suite.
// Resilience pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
