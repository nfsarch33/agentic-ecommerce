package audit

import (
	"sync"
	"time"
)

// AuditEntry is a single immutable audit record.
type AuditEntry struct {
	ID        int64
	Actor     string
	Action    string
	Resource  string
	Metadata  map[string]any
	CreatedAt time.Time
}

// AuditFilter selects entries from the log.
type AuditFilter struct {
	Actor    string
	Resource string
	Since    time.Time
	Until    time.Time
}

// AuditLogger provides an append-only audit trail.
type AuditLogger struct {
	mu      sync.RWMutex
	entries []AuditEntry
	nextID  int64
}

func NewAuditLogger() *AuditLogger {
	return &AuditLogger{}
}

// Log records an audit event at the current time.
func (l *AuditLogger) Log(actor, action, resource string, metadata map[string]any) {
	l.LogAt(actor, action, resource, metadata, time.Now().UTC())
}

// LogAt records an audit event at the specified time (used in tests for time control).
func (l *AuditLogger) LogAt(actor, action, resource string, metadata map[string]any, at time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.nextID++
	l.entries = append(l.entries, AuditEntry{
		ID:        l.nextID,
		Actor:     actor,
		Action:    action,
		Resource:  resource,
		Metadata:  copyMeta(metadata),
		CreatedAt: at,
	})
}

// Query returns entries matching the filter. Returns copies to preserve immutability.
func (l *AuditLogger) Query(f AuditFilter) []AuditEntry {
	l.mu.RLock()
	defer l.mu.RUnlock()

	out := make([]AuditEntry, 0)
	for _, e := range l.entries {
		if f.Actor != "" && e.Actor != f.Actor {
			continue
		}
		if f.Resource != "" && e.Resource != f.Resource {
			continue
		}
		if !f.Since.IsZero() && e.CreatedAt.Before(f.Since) {
			continue
		}
		if !f.Until.IsZero() && e.CreatedAt.After(f.Until) {
			continue
		}
		out = append(out, copyEntry(e))
	}
	return out
}

func copyEntry(e AuditEntry) AuditEntry {
	return AuditEntry{
		ID:        e.ID,
		Actor:     e.Actor,
		Action:    e.Action,
		Resource:  e.Resource,
		Metadata:  copyMeta(e.Metadata),
		CreatedAt: e.CreatedAt,
	}
}

func copyMeta(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}
