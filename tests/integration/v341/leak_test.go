//go:build v341_smoke

package v341

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.4.1 EC-4-3 multichannel fan-out smoke fails the suite. The
// lifecycle.Manager drain at end-of-test must release every
// goroutine spawned by the in-memory eventbus dispatch loop +
// workerpool fan-out + per-channel stub HTTP cassettes.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
