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
	goleak.VerifyTestMain(m)
}
