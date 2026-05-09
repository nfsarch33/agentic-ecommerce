package enrichment

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.2.0 enrichment package fails the suite. EC-2-1 + EC-2-3
// resilience pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
