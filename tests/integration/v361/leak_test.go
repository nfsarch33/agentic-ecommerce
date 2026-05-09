//go:build v361_smoke

package v361

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.6.1 EC-8-1 50-fixture, EC-8-3 inbound->reply E2E,
// EC-9-1 GMV load, or EC-9-2 SSE load smoke fails the suite.
//
// The lifecycle.Manager + workerpool drain at end-of-test must
// release every goroutine spawned by:
//   - the in-memory eventbus dispatch loop (EC-8-3 + EC-9-2);
//   - the SSE per-connection dispatch goroutines (EC-9-2);
//   - the testcontainers-go Postgres adapter (EC-9-1);
//   - the per-channel adapter mock httptest servers (EC-8-3).
//
// goleak ignores known stable-noise goroutines (testcontainers
// bookkeeping background workers) so the production-shape leak
// signal isn't swamped by infra goroutines.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// testcontainers-go starts a docker SDK reaper goroutine
		// that lives across tests; safe to ignore in a v361_smoke
		// suite because the reaper is process-scoped.
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
	)
}
