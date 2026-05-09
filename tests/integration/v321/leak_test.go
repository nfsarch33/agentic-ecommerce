//go:build v321_smoke

package v321

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.2.1 50-product smoke fails the suite. The lifecycle.Manager
// drain at end-of-test must release every goroutine spawned by the
// workerpool, the rag.TrendIngestor fan-out, and the per-stage
// agents.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
