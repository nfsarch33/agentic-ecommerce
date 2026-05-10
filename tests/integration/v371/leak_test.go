//go:build v371_smoke

package v371

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v3.7.1 EC-10-2 memory-pressure smoke, EC-10-3 rate-limit
// drain smoke, or EC-10-4 CAPTCHA pause-resume E2E fails the
// suite. Resilience pillar gate (13-sprint streak target).
//
// The lifecycle.Manager + workerpool drain at end-of-test must
// release every goroutine spawned by:
//   - the in-memory eventbus dispatch loop (CAPTCHADetectedEvent
//   - RateLimitDrainEvent + OmniParserMemoryPausedEvent +
//     OmniParserUnavailableEvent emitters);
//   - the mock omniparser-bridge httptest server (Task 1 + Task 3);
//   - the per-scenario ratelimit jitter Allow goroutines (Task 2);
//   - the per-scenario CAPTCHA WaitResolved goroutines (Task 3);
//   - any test-local goroutines spawned by t.Cleanup hooks.
//
// goleak ignores known stable-noise goroutines (testcontainers
// bookkeeping background workers + httptest server.Serve) so the
// production-shape leak signal isn't swamped by infra goroutines.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// testcontainers-go starts a docker SDK reaper goroutine
		// that lives across tests; safe to ignore in a v371_smoke
		// suite because the reaper is process-scoped.
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
	)
}
