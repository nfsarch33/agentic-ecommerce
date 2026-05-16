package marketplacesync

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
)

var (
	ErrEngineUnconfigured = errors.New("marketplacesync: engine unconfigured")
	ErrInvalidEvent       = errors.New("marketplacesync: invalid event")
	ErrSyncFailed         = errors.New("marketplacesync: sync failed")
)

type EntityType string

const (
	EntityProduct EntityType = "product"
)

type Operation string

const (
	OperationUpsert Operation = "upsert"
	OperationDelete Operation = "delete"
)

type SyncStatus string

const (
	StatusApplied   SyncStatus = "applied"
	StatusDuplicate SyncStatus = "duplicate"
	StatusDLQ       SyncStatus = "dlq"
)

type ProductEvent struct {
	TenantID   string
	Provider   string
	EntityType EntityType
	EntityID   string
	ExternalID string
	Operation  Operation
	Version    string
	Payload    map[string]any
}

type ApplyResult struct {
	RemoteID string
	Version  string
}

type SyncResult struct {
	Status   SyncStatus
	RemoteID string
	Attempts int
}

type DLQRecord struct {
	ID       string
	Event    ProductEvent
	Attempts int
	Reason   string
}

type Connector interface {
	Apply(context.Context, ProductEvent) (ApplyResult, error)
}

type Ledger interface {
	IsCompleted(context.Context, string) (bool, error)
	MarkCompleted(context.Context, string, ApplyResult) error
}

type DLQ interface {
	Enqueue(context.Context, DLQRecord) error
}

type Metrics interface {
	RecordSyncEvent(ProductEvent, SyncStatus)
	RecordDLQ(DLQRecord)
	RecordReplay(DLQRecord, SyncStatus)
}

type EngineConfig struct {
	Connector   Connector
	Ledger      Ledger
	DLQ         DLQ
	Metrics     Metrics
	MaxAttempts int
}

type Engine struct {
	connector   Connector
	ledger      Ledger
	dlq         DLQ
	metrics     Metrics
	maxAttempts int
	locksMu     sync.Mutex
	locks       map[string]*keyLock
}

type keyLock struct {
	mu   sync.Mutex
	refs int
}

func NewEngine(cfg EngineConfig) (*Engine, error) {
	if cfg.Connector == nil {
		return nil, fmt.Errorf("%w: connector required", ErrEngineUnconfigured)
	}
	if cfg.Ledger == nil {
		return nil, fmt.Errorf("%w: ledger required", ErrEngineUnconfigured)
	}
	if cfg.DLQ == nil {
		return nil, fmt.Errorf("%w: dlq required", ErrEngineUnconfigured)
	}
	if cfg.MaxAttempts <= 0 {
		cfg.MaxAttempts = 1
	}
	return &Engine{
		connector:   cfg.Connector,
		ledger:      cfg.Ledger,
		dlq:         cfg.DLQ,
		metrics:     cfg.Metrics,
		maxAttempts: cfg.MaxAttempts,
	}, nil
}

func (e *Engine) Sync(ctx context.Context, event ProductEvent) (SyncResult, error) {
	if err := event.validate(); err != nil {
		return SyncResult{}, err
	}
	key := event.key()
	release := e.acquireKey(key)
	defer release()

	done, err := e.ledger.IsCompleted(ctx, key)
	if err != nil {
		return SyncResult{}, err
	}
	if done {
		e.recordSync(event, StatusDuplicate)
		return SyncResult{Status: StatusDuplicate}, nil
	}

	var lastErr error
	for attempt := 1; attempt <= e.maxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return SyncResult{}, err
		}
		applied, err := e.connector.Apply(ctx, event)
		if err == nil {
			if err := e.ledger.MarkCompleted(ctx, key, applied); err != nil {
				return SyncResult{}, err
			}
			e.recordSync(event, StatusApplied)
			return SyncResult{Status: StatusApplied, RemoteID: applied.RemoteID, Attempts: attempt}, nil
		}
		lastErr = err
	}

	record := DLQRecord{Event: event, Attempts: e.maxAttempts, Reason: lastErr.Error()}
	if err := e.dlq.Enqueue(ctx, record); err != nil {
		return SyncResult{}, err
	}
	if e.metrics != nil {
		e.metrics.RecordDLQ(record)
	}
	e.recordSync(event, StatusDLQ)
	return SyncResult{Status: StatusDLQ, Attempts: e.maxAttempts}, fmt.Errorf("%w: %w", ErrSyncFailed, lastErr)
}

func (e *Engine) Replay(ctx context.Context, record DLQRecord) (SyncResult, error) {
	result, err := e.Sync(ctx, record.Event)
	if e.metrics != nil {
		e.metrics.RecordReplay(record, result.Status)
	}
	return result, err
}

func (e *Engine) recordSync(event ProductEvent, status SyncStatus) {
	if e.metrics == nil {
		return
	}
	e.metrics.RecordSyncEvent(event, status)
}

func (e *Engine) acquireKey(key string) func() {
	e.locksMu.Lock()
	if e.locks == nil {
		e.locks = make(map[string]*keyLock)
	}
	lock := e.locks[key]
	if lock == nil {
		lock = &keyLock{}
		e.locks[key] = lock
	}
	lock.refs++
	e.locksMu.Unlock()

	lock.mu.Lock()
	return func() {
		lock.mu.Unlock()
		e.locksMu.Lock()
		lock.refs--
		if lock.refs == 0 {
			delete(e.locks, key)
		}
		e.locksMu.Unlock()
	}
}

func (e ProductEvent) validate() error {
	switch {
	case strings.TrimSpace(e.TenantID) == "":
		return fmt.Errorf("%w: tenant id required", ErrInvalidEvent)
	case strings.TrimSpace(e.Provider) == "":
		return fmt.Errorf("%w: provider required", ErrInvalidEvent)
	case strings.TrimSpace(e.EntityID) == "":
		return fmt.Errorf("%w: entity id required", ErrInvalidEvent)
	case e.EntityType == "":
		return fmt.Errorf("%w: entity type required", ErrInvalidEvent)
	case e.Operation == "":
		return fmt.Errorf("%w: operation required", ErrInvalidEvent)
	case strings.TrimSpace(e.Version) == "":
		return fmt.Errorf("%w: version required", ErrInvalidEvent)
	default:
		return nil
	}
}

func (e ProductEvent) key() string {
	return strings.Join([]string{
		e.TenantID,
		e.Provider,
		string(e.EntityType),
		e.EntityID,
		string(e.Operation),
		e.Version,
	}, "\x00")
}
