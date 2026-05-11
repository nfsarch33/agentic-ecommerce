package coord

import (
	"testing"
)

type capturingEmitter struct {
	calls []map[string]string
}

func (e *capturingEmitter) Inc(labels map[string]string) {
	cp := make(map[string]string, len(labels))
	for k, v := range labels {
		cp[k] = v
	}
	e.calls = append(e.calls, cp)
}

func TestMetricsAdapter_NilEmitterReturnsNil(t *testing.T) {
	t.Parallel()
	if a := NewMetricsAdapter(nil); a != nil {
		t.Fatalf("NewMetricsAdapter(nil) = %v want nil", a)
	}
}

func TestMetricsAdapter_NilSafeRecord(t *testing.T) {
	t.Parallel()
	var a *MetricsAdapter
	a.RecordCoordinationConflict("t", "p", "f", "last_write_wins")
}

func TestMetricsAdapter_RecordsLabelTuple(t *testing.T) {
	t.Parallel()
	emit := &capturingEmitter{}
	a := NewMetricsAdapter(emit)
	if a == nil {
		t.Fatal("NewMetricsAdapter returned nil for non-nil emitter")
	}
	a.RecordCoordinationConflict("tenant-1", "PricingAgent", "FulfilmentAgent", "last_write_wins")
	if len(emit.calls) != 1 {
		t.Fatalf("calls=%d want 1", len(emit.calls))
	}
	got := emit.calls[0]
	want := map[string]string{
		"tenant_id":  "tenant-1",
		"agent_a":    "PricingAgent",
		"agent_b":    "FulfilmentAgent",
		"resolution": "last_write_wins",
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("label %q = %q want %q (full: %v)", k, got[k], v, got)
		}
	}
}

func TestMetricsAdapter_ImplementsCoordinatorMetrics(t *testing.T) {
	t.Parallel()
	var _ CoordinatorMetrics = (*MetricsAdapter)(nil)
}
