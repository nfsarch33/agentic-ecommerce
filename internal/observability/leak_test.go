package observability

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.2.0 EC-2-5 enrichment metrics + existing tracing helpers
// fails the suite.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
