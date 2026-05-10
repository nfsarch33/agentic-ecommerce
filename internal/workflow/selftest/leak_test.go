package selftest

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any goroutine leak in the
// v3.8.0 selftest package fails the suite. Resilience pillar gate.
//
// The Temporal SDK's WorkflowReplayer spawns internal goroutines for
// timer + replay coordination; the IgnoreTopFunction filters mirror
// those in internal/workflow/leak_test.go.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreTopFunction("go.temporal.io/sdk/internal.(*sessionEnvironmentImpl).completeSession"),
		goleak.IgnoreTopFunction("go.temporal.io/sdk/internal.(*workflowEnvironmentImpl).RegisterDelayedCallback.func1"),
		goleak.IgnoreTopFunction("go.temporal.io/sdk/internal.(*WorkflowReplayer).ReplayWorkflowHistoryFromJSONFile.func1"),
	)
}
