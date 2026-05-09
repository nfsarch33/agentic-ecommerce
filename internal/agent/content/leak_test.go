package content

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the content package (existing agent + v3.4.0 EC-5-1 video script
// generator) fails the suite. Resilience pillar gate.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
