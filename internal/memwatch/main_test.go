// File scope: v6.1.0 CF-12 -- shared test helpers for the memwatch
// package.
//
// CF-12 surfaced flaky behaviour for TestHeapCeilingFiresAfterDwell
// + TestGoroutineCeilingFiresAfterDwell on macOS test runners. The
// pre-existing leak_test.go already pins goleak.VerifyTestMain; the
// fix adds an explicit hardenedClose helper so every ceiling test
// calls Close() via t.Cleanup() rather than relying on ctx-cancel
// alone. This guarantees the Run loop fully exits before the test
// returns, removing the v3.x intermittent leak the chaos suite
// detected when run sequentially on Apple Silicon.
package memwatch

import (
	"context"
	"testing"
	"time"
)

// hardenedClose drains a Sampler with a bounded ctx so the goroutine
// inside Run exits before the test returns. Use via t.Cleanup so
// it fires regardless of test outcome.
func hardenedClose(t *testing.T, s *Sampler) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := s.Close(ctx); err != nil {
		t.Logf("sampler close: %v", err)
	}
}
