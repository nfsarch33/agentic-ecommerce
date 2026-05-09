package lifecycle

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in
// lifecycle.Manager test suites fails the package. v2.10.0 Story 3
// guard rail.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
