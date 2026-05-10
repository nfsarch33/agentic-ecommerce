// File scope: v3.9.1 EC-4-4 -- shared metrics port + helper for the
// Instagram + Pinterest stub adapters.
//
// The stubs intentionally have no external I/O so the only signal
// dashboards see today is the operations-by-tenant counter. Once
// production-ready adapters land in v4.1.x they swap the
// implementation in for a real client without changing the metric
// label set.
package social

import (
	"strings"
	"sync"
)

// StubChannelMetrics is the small port the Instagram + Pinterest
// stub adapters emit operation-by-tenant counters through. cmd/*
// binaries wire the production observability adapter (V391Metrics)
// in; tests pass an in-memory recorder.
type StubChannelMetrics interface {
	RecordStubChannelCall(tenantID, channel, op string)
}

// recordingStubMetrics is the test-only in-memory recorder used by
// the stub adapter tests. Goroutine-safe so the bench tests can
// fan-out without a race.
type recordingStubMetrics struct {
	mu  sync.Mutex
	ops []string
}

func (r *recordingStubMetrics) RecordStubChannelCall(tenantID, channel, op string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ops = append(r.ops, strings.Join([]string{tenantID, channel, op}, "|"))
}

func (r *recordingStubMetrics) calls() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.ops)
}

func (r *recordingStubMetrics) observedOp(op string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range r.ops {
		if strings.HasSuffix(e, "|"+op) {
			return true
		}
	}
	return false
}

func (r *recordingStubMetrics) list() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, len(r.ops))
	copy(out, r.ops)
	return out
}
