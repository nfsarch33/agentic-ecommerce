package marketplacesync

import (
	"context"
	"errors"
	"testing"
)

func TestEngineSyncSkipsDuplicateEventWithoutConnectorCall(t *testing.T) {
	ctx := context.Background()
	ledger := NewInMemoryLedger()
	connector := &recordingConnector{}
	engine := mustEngine(t, EngineConfig{
		Connector: connector,
		Ledger:    ledger,
		DLQ:       NewInMemoryDLQ(),
		Metrics:   newRecordingMetrics(),
	})
	event := ProductEvent{
		TenantID:  "tenant-a",
		Provider:  "woocommerce",
		EntityType: EntityProduct,
		EntityID:   "sku-1",
		ExternalID: "remote-1",
		Operation:  OperationUpsert,
		Version:    "v1",
	}

	first, err := engine.Sync(ctx, event)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if first.Status != StatusApplied {
		t.Fatalf("first status = %s, want %s", first.Status, StatusApplied)
	}

	second, err := engine.Sync(ctx, event)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Status != StatusDuplicate {
		t.Fatalf("second status = %s, want %s", second.Status, StatusDuplicate)
	}
	if connector.calls != 1 {
		t.Fatalf("connector calls = %d, want 1", connector.calls)
	}
}

func TestEngineSyncRetriesThenWritesDLQ(t *testing.T) {
	ctx := context.Background()
	dlq := NewInMemoryDLQ()
	metrics := newRecordingMetrics()
	connector := &recordingConnector{failuresBeforeSuccess: 3, err: errTransient}
	engine := mustEngine(t, EngineConfig{
		Connector:   connector,
		Ledger:      NewInMemoryLedger(),
		DLQ:         dlq,
		Metrics:     metrics,
		MaxAttempts: 2,
	})

	result, err := engine.Sync(ctx, ProductEvent{
		TenantID:  "tenant-a",
		Provider:  "shopify",
		EntityType: EntityProduct,
		EntityID:   "sku-2",
		ExternalID: "remote-2",
		Operation:  OperationUpsert,
		Version:    "v1",
	})
	if !errors.Is(err, ErrSyncFailed) {
		t.Fatalf("error = %v, want ErrSyncFailed", err)
	}
	if result.Status != StatusDLQ {
		t.Fatalf("status = %s, want %s", result.Status, StatusDLQ)
	}
	if connector.calls != 2 {
		t.Fatalf("connector calls = %d, want 2", connector.calls)
	}
	records := dlq.Records()
	if len(records) != 1 {
		t.Fatalf("dlq records = %d, want 1", len(records))
	}
	if records[0].Attempts != 2 || records[0].Reason == "" {
		t.Fatalf("dlq record = %+v, want attempts=2 and reason", records[0])
	}
	if metrics.dlqTotal != 1 {
		t.Fatalf("dlq metric = %d, want 1", metrics.dlqTotal)
	}
}

func TestReplaySkipsAlreadyCompletedEvent(t *testing.T) {
	ctx := context.Background()
	connector := &recordingConnector{}
	metrics := newRecordingMetrics()
	engine := mustEngine(t, EngineConfig{
		Connector: connector,
		Ledger:    NewInMemoryLedger(),
		DLQ:       NewInMemoryDLQ(),
		Metrics:   metrics,
	})
	event := ProductEvent{
		TenantID:  "tenant-a",
		Provider:  "shopify",
		EntityType: EntityProduct,
		EntityID:   "sku-3",
		ExternalID: "remote-3",
		Operation:  OperationUpsert,
		Version:    "v1",
	}
	if _, err := engine.Sync(ctx, event); err != nil {
		t.Fatalf("sync: %v", err)
	}

	result, err := engine.Replay(ctx, DLQRecord{Event: event, Attempts: 2, Reason: "previous transient"})
	if err != nil {
		t.Fatalf("replay: %v", err)
	}
	if result.Status != StatusDuplicate {
		t.Fatalf("replay status = %s, want %s", result.Status, StatusDuplicate)
	}
	if connector.calls != 1 {
		t.Fatalf("connector calls = %d, want 1", connector.calls)
	}
	if metrics.replayTotal != 1 {
		t.Fatalf("replay metric = %d, want 1", metrics.replayTotal)
	}
}

func TestReconcileReportsMismatchedRemoteState(t *testing.T) {
	report := Reconcile([]SnapshotItem{
		{EntityID: "sku-1", Version: "v1"},
		{EntityID: "sku-2", Version: "v2"},
	}, []SnapshotItem{
		{EntityID: "sku-1", Version: "v1"},
		{EntityID: "sku-2", Version: "stale"},
		{EntityID: "sku-remote-only", Version: "v1"},
	})

	if report.TotalLocal != 2 || report.TotalRemote != 3 {
		t.Fatalf("totals = local %d remote %d, want 2 and 3", report.TotalLocal, report.TotalRemote)
	}
	if len(report.Mismatches) != 2 {
		t.Fatalf("mismatches = %d, want 2 (%+v)", len(report.Mismatches), report.Mismatches)
	}
	if report.Mismatches[0].Reason != MismatchVersion {
		t.Fatalf("first mismatch reason = %s, want %s", report.Mismatches[0].Reason, MismatchVersion)
	}
	if report.Mismatches[1].Reason != MismatchRemoteOnly {
		t.Fatalf("second mismatch reason = %s, want %s", report.Mismatches[1].Reason, MismatchRemoteOnly)
	}
}

var errTransient = errors.New("transient")

type recordingConnector struct {
	calls                 int
	failuresBeforeSuccess int
	err                   error
}

func (c *recordingConnector) Apply(_ context.Context, _ ProductEvent) (ApplyResult, error) {
	c.calls++
	if c.calls <= c.failuresBeforeSuccess {
		return ApplyResult{}, c.err
	}
	return ApplyResult{RemoteID: "remote-ok"}, nil
}

type recordingMetrics struct {
	eventsTotal int
	dlqTotal    int
	replayTotal int
}

func newRecordingMetrics() *recordingMetrics { return &recordingMetrics{} }

func (m *recordingMetrics) RecordSyncEvent(_ ProductEvent, _ SyncStatus) { m.eventsTotal++ }
func (m *recordingMetrics) RecordDLQ(_ DLQRecord)                        { m.dlqTotal++ }
func (m *recordingMetrics) RecordReplay(_ DLQRecord, _ SyncStatus)       { m.replayTotal++ }

func mustEngine(t *testing.T, cfg EngineConfig) *Engine {
	t.Helper()
	engine, err := NewEngine(cfg)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	return engine
}
