package agentrace

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain runs goleak across the agentrace package so the writer
// goroutine and the bounded-ring producer cannot silently survive.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
