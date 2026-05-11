package v621

import (
	"testing"

	"go.uber.org/goleak"
)

// TestMain wires goleak.VerifyTestMain so the v6.2.1 observability
// integration tests share the workerpool/breaker/coord guarantee
// that no background goroutine survives the test binary.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}
