//go:build v331_smoke

package v331

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.3.1 EC-3 TikTok sandbox smoke fails the suite. The
// lifecycle.Manager drain at end-of-test must release every
// goroutine spawned by the in-memory eventbus dispatch loop +
// HTTP sandbox client.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
