package social

import "testing"

func TestRecordingStubMetricsRecordsCopiesAndMatchesOperations(t *testing.T) {
	t.Parallel()

	rec := &recordingStubMetrics{}
	rec.RecordStubChannelCall("tenant-a", "instagram", "publish")
	rec.RecordStubChannelCall("tenant-a", "pinterest", "sync")

	if got := rec.calls(); got != 2 {
		t.Fatalf("calls = %d, want 2", got)
	}
	if !rec.observedOp("publish") || !rec.observedOp("sync") {
		t.Fatalf("expected publish and sync operations in %#v", rec.list())
	}
	if rec.observedOp("delete") {
		t.Fatalf("delete should not be observed in %#v", rec.list())
	}

	list := rec.list()
	list[0] = "mutated"
	if rec.list()[0] == "mutated" {
		t.Fatalf("list should return a defensive copy")
	}
}
