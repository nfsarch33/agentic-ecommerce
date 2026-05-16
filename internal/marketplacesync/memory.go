package marketplacesync

import (
	"context"
	"sync"

	"github.com/google/uuid"
)

type InMemoryLedger struct {
	mu        sync.Mutex
	completed map[string]ApplyResult
}

func NewInMemoryLedger() *InMemoryLedger {
	return &InMemoryLedger{completed: map[string]ApplyResult{}}
}

func (l *InMemoryLedger) IsCompleted(_ context.Context, key string) (bool, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	_, ok := l.completed[key]
	return ok, nil
}

func (l *InMemoryLedger) MarkCompleted(_ context.Context, key string, result ApplyResult) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.completed[key] = result
	return nil
}

type InMemoryDLQ struct {
	mu      sync.Mutex
	records []DLQRecord
}

func NewInMemoryDLQ() *InMemoryDLQ { return &InMemoryDLQ{} }

func (q *InMemoryDLQ) Enqueue(_ context.Context, record DLQRecord) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	if record.ID == "" {
		record.ID = uuid.NewString()
	}
	q.records = append(q.records, record)
	return nil
}

func (q *InMemoryDLQ) Records() []DLQRecord {
	q.mu.Lock()
	defer q.mu.Unlock()
	out := make([]DLQRecord, len(q.records))
	copy(out, q.records)
	return out
}

func (q *InMemoryDLQ) Record(id string) (DLQRecord, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	for _, record := range q.records {
		if record.ID == id {
			return record, true
		}
	}
	return DLQRecord{}, false
}

func (q *InMemoryDLQ) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	return len(q.records)
}
