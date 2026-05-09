//go:build v351_smoke

package v351

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.5.1 EC-6-1 cost-change + EC-7-2 dropship saga smoke fails the
// suite. The lifecycle.Manager drain at end-of-test must release
// every goroutine spawned by the in-memory eventbus dispatch loop,
// the supplier mock httptest servers, and any worker fan-out the
// agent + monitor wire internally.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
