package workerpool

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain. v2.10.0 Story 3 guard rail
// against unbounded worker leaks.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
