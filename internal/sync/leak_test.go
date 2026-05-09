package sync

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// internal/sync package fails the suite. v3.3.0 EC-3-4 resilience
// pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
