//go:build v381_smoke

package v381

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.8.1 EC-7-3 shipping label acceptance smoke, EC-7-4 status
// propagation E2E, EC-7-5 returns saga validation, or EC-9-3 ROI
// heatmap load test fails the suite. Resilience pillar gate
// (15-sprint streak target, sprint v381).
//
// The lifecycle.Manager + workerpool drain at end-of-test must
// release every goroutine spawned by:
//   - the in-memory eventbus dispatch loop (ShipmentLabelGenerated
//   - ShipmentStatusUpdated + ReturnsSagaPayload emitters);
//   - the carrier webhook httptest servers (Task 1 + Task 2);
//   - the per-scenario StatusPropagator retry sleep helpers
//     (Task 2 wires Sleep=func(time.Duration){} so jitter never
//     spawns timers);
//   - the per-scenario Temporal testsuite workflow environment
//     (Task 3 -- the testsuite owns its own goroutine pool that
//     winds down inside ExecuteWorkflow);
//   - the per-scenario ROIRepository load goroutines (Task 4);
//   - any test-local goroutines spawned by t.Cleanup hooks.
//
// goleak ignores known stable-noise goroutines (testcontainers
// reaper background workers + httptest server.Serve) so the
// production-shape leak signal isn't swamped by infra goroutines.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// testcontainers-go starts a docker SDK reaper goroutine
		// that lives across tests; safe to ignore in a v381_smoke
		// suite because the reaper is process-scoped.
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
	)
}
