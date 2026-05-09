package media

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.2.0 EC-2-2 product image pipeline (and existing media
// processor / intelligence helpers) fails the suite.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
