// Package coord -- v4.7.0 CoordinationLog: append-only NDJSON log
// of all coordination decisions for offline analysis.
//
// Log path: ~/.agentrace/coordination.ndjson (ties into the
// Agentrace observability spine from Phase B).
//
// Decomposition discipline (HARD GATE: complex_fn=4):
//
//   - Append        -> marshal + write (cyclomatic 3)
//   - ensureDir     -> mkdir (cyclomatic 2)
//   - Close         -> flush + close (cyclomatic 2)
package coord

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// CoordinationLogEntry is one NDJSON line in the coordination log.
type CoordinationLogEntry struct {
	Timestamp    time.Time              `json:"timestamp"`
	TenantID     string                 `json:"tenant_id"`
	SKU          string                 `json:"sku"`
	Agents       []string               `json:"agents"`
	ConflictType string                 `json:"conflict_type"`
	Resolution   string                 `json:"resolution"`
	PolicyName   string                 `json:"policy_name"`
	ChosenAgent  string                 `json:"chosen_agent"`
	RewardValue  float64                `json:"reward_value,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
}

// CoordinationLog writes append-only NDJSON entries for offline
// analysis. Thread-safe; callers may invoke Append from multiple
// goroutines.
type CoordinationLog struct {
	mu   sync.Mutex
	file *os.File
	path string
}

// DefaultCoordinationLogPath returns ~/.agentrace/coordination.ndjson.
func DefaultCoordinationLogPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "/tmp"
	}
	return filepath.Join(home, ".agentrace", "coordination.ndjson")
}

// NewCoordinationLog opens (or creates) the log file at the given
// path. The parent directory is created if absent.
func NewCoordinationLog(path string) (*CoordinationLog, error) {
	if err := ensureLogDir(path); err != nil {
		return nil, fmt.Errorf("coord: ensure log dir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("coord: open log: %w", err)
	}
	return &CoordinationLog{file: f, path: path}, nil
}

// Append writes one NDJSON line to the log. Thread-safe.
func (cl *CoordinationLog) Append(entry CoordinationLogEntry) error {
	data, err := json.Marshal(entry)
	if err != nil {
		return fmt.Errorf("coord: marshal log entry: %w", err)
	}
	data = append(data, '\n')
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if _, err := cl.file.Write(data); err != nil {
		return fmt.Errorf("coord: write log entry: %w", err)
	}
	return nil
}

// Path returns the log file path.
func (cl *CoordinationLog) Path() string { return cl.path }

// Close flushes and closes the log file.
func (cl *CoordinationLog) Close() error {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	if cl.file == nil {
		return nil
	}
	return cl.file.Close()
}

// InMemoryCoordinationLog captures entries in memory for testing.
type InMemoryCoordinationLog struct {
	mu      sync.Mutex
	entries []CoordinationLogEntry
}

// Append stores the entry in memory.
func (m *InMemoryCoordinationLog) Append(entry CoordinationLogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, entry)
	return nil
}

// Entries returns a copy of all appended entries.
func (m *InMemoryCoordinationLog) Entries() []CoordinationLogEntry {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]CoordinationLogEntry, len(m.entries))
	copy(out, m.entries)
	return out
}

func ensureLogDir(path string) error {
	dir := filepath.Dir(path)
	return os.MkdirAll(dir, 0755)
}
