package memwatch

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain. v2.10.0 Story 3 guard rail.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
