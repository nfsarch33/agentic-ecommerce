// File scope: tiny per-tenant SSE connection counter shared by the
// V360Metrics adapter. Lives in a separate file so the V360Metrics
// receivers stay readable.
package observability

import "sync"

// sseConnectionsCounter is a process-wide counter the V360Metrics
// SSE adapter consults so the gauge re-Set has the latest delta.
// The process scope is correct because the SSE handler owns the
// connection lifecycle inside a single process; horizontal scale
// uses a per-binary registry, so the global counter never crosses
// processes.
var sseConnectionsCounter = newSSECounter()

type ssePerTenantCounter struct {
	mu    sync.Mutex
	state map[string]float64
}

func newSSECounter() *ssePerTenantCounter {
	return &ssePerTenantCounter{state: map[string]float64{}}
}

func (c *ssePerTenantCounter) adjust(tenantID string, delta float64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.state[tenantID] += delta
	if c.state[tenantID] < 0 {
		c.state[tenantID] = 0
	}
}

func (c *ssePerTenantCounter) get(tenantID string) float64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.state[tenantID]
}
