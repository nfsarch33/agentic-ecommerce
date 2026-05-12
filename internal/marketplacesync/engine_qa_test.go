package marketplacesync

import (
	"context"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"
)

func TestEngineConcurrentDuplicateSyncOnlyAppliesOnce(t *testing.T) {
	ctx := context.Background()
	connector := &slowRecordingConnector{delay: 10 * time.Millisecond}
	engine := mustEngine(t, EngineConfig{
		Connector: connector,
		Ledger:    NewInMemoryLedger(),
		DLQ:       NewInMemoryDLQ(),
		Metrics:   newRecordingMetrics(),
	})
	event := qaProductEvent("sku-concurrent", "v1")

	const workers = 8
	var wg sync.WaitGroup
	results := make(chan SyncStatus, workers)
	errs := make(chan error, workers)
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := engine.Sync(ctx, event)
			if err != nil {
				errs <- err
				return
			}
			results <- result.Status
		}()
	}
	wg.Wait()
	close(results)
	close(errs)

	if len(errs) > 0 {
		t.Fatalf("sync returned errors: %v", <-errs)
	}
	var applied, duplicate int
	for status := range results {
		switch status {
		case StatusApplied:
			applied++
		case StatusDuplicate:
			duplicate++
		default:
			t.Fatalf("unexpected status %s", status)
		}
	}
	if applied != 1 || duplicate != workers-1 {
		t.Fatalf("status counts applied=%d duplicate=%d, want 1/%d", applied, duplicate, workers-1)
	}
	if connector.calls != 1 {
		t.Fatalf("connector calls = %d, want 1", connector.calls)
	}
}

func TestEngineRetryOutcomeMatrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                  string
		failuresBeforeSuccess int
		maxAttempts           int
		wantStatus            SyncStatus
		wantErr               bool
		wantAttempts          int
		wantDLQ               int
	}{
		{name: "recovers before max attempts", failuresBeforeSuccess: 1, maxAttempts: 3, wantStatus: StatusApplied, wantAttempts: 2},
		{name: "exhausts retry budget", failuresBeforeSuccess: 3, maxAttempts: 2, wantStatus: StatusDLQ, wantErr: true, wantAttempts: 2, wantDLQ: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dlq := NewInMemoryDLQ()
			connector := &recordingConnector{failuresBeforeSuccess: tt.failuresBeforeSuccess, err: errTransient}
			engine := mustEngine(t, EngineConfig{
				Connector:   connector,
				Ledger:      NewInMemoryLedger(),
				DLQ:         dlq,
				Metrics:     newRecordingMetrics(),
				MaxAttempts: tt.maxAttempts,
			})

			result, err := engine.Sync(context.Background(), qaProductEvent("sku-"+tt.name, "v1"))
			if (err != nil) != tt.wantErr {
				t.Fatalf("err = %v, wantErr %v", err, tt.wantErr)
			}
			if result.Status != tt.wantStatus {
				t.Fatalf("status = %s, want %s", result.Status, tt.wantStatus)
			}
			if result.Attempts != tt.wantAttempts {
				t.Fatalf("attempts = %d, want %d", result.Attempts, tt.wantAttempts)
			}
			if got := len(dlq.Records()); got != tt.wantDLQ {
				t.Fatalf("dlq records = %d, want %d", got, tt.wantDLQ)
			}
		})
	}
}

func TestReplayFixtureAudit(t *testing.T) {
	t.Parallel()

	raw, err := os.ReadFile("testdata/v8_p01_replay_fixture.json")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var records []DLQRecord
	if err := json.Unmarshal(raw, &records); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	if len(records) != 2 {
		t.Fatalf("fixture records = %d, want 2", len(records))
	}

	connector := &recordingConnector{}
	engine := mustEngine(t, EngineConfig{
		Connector: connector,
		Ledger:    NewInMemoryLedger(),
		DLQ:       NewInMemoryDLQ(),
		Metrics:   newRecordingMetrics(),
	})
	for _, record := range records {
		result, err := engine.Replay(context.Background(), record)
		if err != nil {
			t.Fatalf("replay %s: %v", record.Event.EntityID, err)
		}
		if result.Status != StatusApplied {
			t.Fatalf("replay %s status = %s, want %s", record.Event.EntityID, result.Status, StatusApplied)
		}
	}
	if connector.calls != len(records) {
		t.Fatalf("connector calls = %d, want %d", connector.calls, len(records))
	}
}

type slowRecordingConnector struct {
	mu    sync.Mutex
	calls int
	delay time.Duration
}

func (c *slowRecordingConnector) Apply(ctx context.Context, _ ProductEvent) (ApplyResult, error) {
	if c.delay > 0 {
		select {
		case <-time.After(c.delay):
		case <-ctx.Done():
			return ApplyResult{}, ctx.Err()
		}
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.calls++
	return ApplyResult{RemoteID: "remote-ok"}, nil
}

func qaProductEvent(entityID, version string) ProductEvent {
	return ProductEvent{
		TenantID:   "tenant-a",
		Provider:   "shopify",
		EntityType: EntityProduct,
		EntityID:   entityID,
		Operation:  OperationUpsert,
		Version:    version,
	}
}
