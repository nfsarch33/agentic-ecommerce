package coord

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.5.1 Existing #4 MADRL coordinator seed package fails the
// suite. Resilience pillar v2.10 gate (extends the v3.5.0
// internal/agent/fulfilment + internal/billing leak guards).
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
