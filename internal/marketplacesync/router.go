package marketplacesync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

const (
	ReplayStateIdle    = "idle"
	ReplayStateQueued  = "queued"
	ReplayStateApplied = "applied"
	ReplayStateDLQ     = "dlq"
	ReplayStateFailed  = "failed"
)

type RouterConfig struct {
	Connectors  map[string]Connector
	Ledger      Ledger
	DLQ         DLQStore
	Metrics     Metrics
	MaxAttempts int
}

type DLQStore interface {
	DLQ
	Records() []DLQRecord
	Record(string) (DLQRecord, bool)
	Depth() int
}

type ReplaySnapshot struct {
	State     string
	RecordID  string
	UpdatedAt time.Time
}

type StatusSnapshot struct {
	DLQDepth       int
	Replay         ReplaySnapshot
	Reconciliation ReconciliationReport
}

type Router struct {
	dlq     DLQStore
	metrics Metrics

	mu             sync.RWMutex
	engines        map[string]*Engine
	replay         ReplaySnapshot
	reconciliation ReconciliationReport
}

func NewRouter(cfg RouterConfig) (*Router, error) {
	if cfg.Ledger == nil {
		return nil, fmt.Errorf("%w: ledger required", ErrEngineUnconfigured)
	}
	if cfg.DLQ == nil {
		return nil, fmt.Errorf("%w: dlq required", ErrEngineUnconfigured)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 3
	}

	router := &Router{
		dlq:      cfg.DLQ,
		metrics:  cfg.Metrics,
		engines:  make(map[string]*Engine, len(cfg.Connectors)),
		replay:   ReplaySnapshot{State: ReplayStateIdle},
		mu:       sync.RWMutex{},
	}
	for provider, connector := range cfg.Connectors {
		if connector == nil {
			continue
		}
		engine, err := NewEngine(EngineConfig{
			Connector:   connector,
			Ledger:      cfg.Ledger,
			DLQ:         cfg.DLQ,
			Metrics:     cfg.Metrics,
			MaxAttempts: cfg.MaxAttempts,
		})
		if err != nil {
			return nil, fmt.Errorf("provider %s: %w", provider, err)
		}
		router.engines[normalizeProvider(provider)] = engine
	}
	return router, nil
}

func (r *Router) Sync(ctx context.Context, event ProductEvent) (SyncResult, error) {
	if err := event.validate(); err != nil {
		return SyncResult{}, err
	}
	engine, ok := r.engineFor(event.Provider)
	if !ok {
		return r.enqueueDLQ(ctx, DLQRecord{
			Event:    event,
			Attempts: 1,
			Reason:   fmt.Sprintf("unsupported provider: %s", strings.TrimSpace(event.Provider)),
		})
	}
	result, err := engine.Sync(ctx, event)
	if errors.Is(err, ErrSyncFailed) {
		return result, nil
	}
	return result, err
}

func (r *Router) Replay(ctx context.Context, record DLQRecord) (SyncResult, error) {
	if err := record.Event.validate(); err != nil {
		return SyncResult{}, err
	}
	engine, ok := r.engineFor(record.Event.Provider)
	if !ok {
		result, err := r.enqueueDLQ(ctx, DLQRecord{
			Event:    record.Event,
			Attempts: max(record.Attempts, 1),
			Reason:   fmt.Sprintf("unsupported provider: %s", strings.TrimSpace(record.Event.Provider)),
		})
		r.setReplayState(record.ID, replayStateFromSync(result.Status))
		return result, err
	}
	result, err := engine.Replay(ctx, record)
	if errors.Is(err, ErrSyncFailed) {
		err = nil
	}
	r.setReplayState(record.ID, replayStateFromSync(result.Status))
	return result, err
}

func (r *Router) MarkReplayQueued(id string) bool {
	if r == nil || r.dlq == nil {
		return false
	}
	if _, ok := r.dlq.Record(strings.TrimSpace(id)); !ok {
		return false
	}
	r.setReplayState(strings.TrimSpace(id), ReplayStateQueued)
	return true
}

func (r *Router) Records() []DLQRecord {
	if r == nil || r.dlq == nil {
		return nil
	}
	return r.dlq.Records()
}

func (r *Router) Record(id string) (DLQRecord, bool) {
	if r == nil || r.dlq == nil {
		return DLQRecord{}, false
	}
	return r.dlq.Record(id)
}

func (r *Router) Snapshot() StatusSnapshot {
	if r == nil || r.dlq == nil {
		return StatusSnapshot{Replay: ReplaySnapshot{State: ReplayStateIdle}}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return StatusSnapshot{
		DLQDepth:       r.dlq.Depth(),
		Replay:         r.replay,
		Reconciliation: r.reconciliation,
	}
}

func (r *Router) engineFor(provider string) (*Engine, bool) {
	if r == nil {
		return nil, false
	}
	engine, ok := r.engines[normalizeProvider(provider)]
	return engine, ok
}

func (r *Router) enqueueDLQ(ctx context.Context, record DLQRecord) (SyncResult, error) {
	if r == nil || r.dlq == nil {
		return SyncResult{}, fmt.Errorf("%w: dlq required", ErrEngineUnconfigured)
	}
	if err := r.dlq.Enqueue(ctx, record); err != nil {
		return SyncResult{}, err
	}
	if r.metrics != nil {
		r.metrics.RecordDLQ(record)
		r.metrics.RecordSyncEvent(record.Event, StatusDLQ)
	}
	return SyncResult{
		Status:   StatusDLQ,
		Attempts: max(record.Attempts, 1),
	}, nil
}

func (r *Router) setReplayState(recordID, state string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.replay = ReplaySnapshot{
		State:     state,
		RecordID:  strings.TrimSpace(recordID),
		UpdatedAt: time.Now().UTC(),
	}
}

func replayStateFromSync(status SyncStatus) string {
	switch status {
	case StatusApplied:
		return ReplayStateApplied
	case StatusDuplicate:
		return ReplayStateApplied
	case StatusDLQ:
		return ReplayStateDLQ
	default:
		return ReplayStateFailed
	}
}

func normalizeProvider(provider string) string {
	return strings.ToLower(strings.TrimSpace(provider))
}

func max(value, fallback int) int {
	if value > fallback {
		return value
	}
	return fallback
}
