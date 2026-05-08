package marketplace

import "sync"

// settingsStore is the in-memory per-(tenant, slug) settings blob. The
// store is intentionally pluggable behind the Service surface; v2.5.0
// will swap this for a postgres-backed implementation when billing
// requires per-tenant settings durability.
type settingsStore struct {
	mu   sync.RWMutex
	rows map[string]map[string]any
}

func newSettingsStore() *settingsStore {
	return &settingsStore{rows: make(map[string]map[string]any)}
}

func settingsKey(tenantID, slug string) string {
	return tenantID + "::" + slug
}

func (s *settingsStore) get(tenantID, slug string) map[string]any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	row, ok := s.rows[settingsKey(tenantID, slug)]
	if !ok {
		return map[string]any{}
	}
	out := make(map[string]any, len(row))
	for k, v := range row {
		out[k] = v
	}
	return out
}

func (s *settingsStore) set(tenantID, slug string, values map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if values == nil {
		delete(s.rows, settingsKey(tenantID, slug))
		return
	}
	row := make(map[string]any, len(values))
	for k, v := range values {
		row[k] = v
	}
	s.rows[settingsKey(tenantID, slug)] = row
}
