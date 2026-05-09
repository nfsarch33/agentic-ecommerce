package workflow

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in
// the v2.2.0 + v3.1.0 + v3.5.0 Temporal workflow package fails
// the suite. v3.5.0 EC-7-1 order aggregator + EC-7-2 dropship
// extensions inherit this gate.
//
// Temporal SDK's testsuite spawns internal goroutines (gRPC
// callbacks, replay timers); the IgnoreTopFunction filter excludes
// the well-known long-lived runtime goroutines so legitimate test
// code paths surface.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("go.temporal.io/sdk/internal.(*sessionEnvironmentImpl).completeSession"),
		goleak.IgnoreTopFunction("go.temporal.io/sdk/internal.(*workflowEnvironmentImpl).RegisterDelayedCallback.func1"),
		goleak.IgnoreTopFunction("go.temporal.io/sdk/internal.(*WorkflowReplayer).ReplayWorkflowHistoryFromJSONFile.func1"),
	)
}
