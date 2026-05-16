package rag

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so any leaked goroutine in
// the rag package fails the suite. v3.2.0 EC-2-4 resilience pillar
// gate. Also covers the v3.2.0 TrendIngestor.fetchAll fan-out path
// which uses internal/workerpool (no raw goroutines) but spawns a
// drain goroutine that must exit on every code path.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		// Testcontainers leaves the Ryuk reaper connection goroutine alive briefly
		// after container cleanup on Docker Desktop. Keep goleak enabled for the
		// package, but ignore this known external test harness goroutine.
		goleak.IgnoreTopFunction("github.com/testcontainers/testcontainers-go.(*Reaper).connect.func1"),
	)
}
