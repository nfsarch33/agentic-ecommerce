package webhook

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.3.0 EC-3-3 webhook package fails the suite. Resilience pillar
// gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
