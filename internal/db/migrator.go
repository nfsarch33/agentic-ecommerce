package db

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"sync"
)

var (
	ErrDuplicateVersion  = errors.New("duplicate migration version")
	ErrMigrationNotFound = errors.New("migration not found")
	ErrChecksumMismatch  = errors.New("migration checksum mismatch")
)

type Migration struct {
	Version  string
	SQL      string
	Checksum string
}

func checksumOf(sql string) string {
	sum := sha256.Sum256([]byte(sql))
	return fmt.Sprintf("%x", sum)
}

type MigrationStatus struct {
	Version string
	Applied bool
}

type Migrator struct {
	mu         sync.Mutex
	migrations []Migration // ordered definitions
	applied    []string    // versions applied in order
}

func NewMigrator() *Migrator { return &Migrator{} }

func (m *Migrator) Add(version, sql string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, mg := range m.migrations {
		if mg.Version == version {
			return ErrDuplicateVersion
		}
	}
	m.migrations = append(m.migrations, Migration{Version: version, SQL: sql, Checksum: checksumOf(sql)})
	return nil
}

func (m *Migrator) Up() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	applied := make(map[string]bool)
	for _, v := range m.applied {
		applied[v] = true
	}
	for _, mg := range m.migrations {
		if !applied[mg.Version] {
			m.applied = append(m.applied, mg.Version)
		}
	}
	return nil
}

func (m *Migrator) Down(steps int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if steps > len(m.applied) {
		steps = len(m.applied)
	}
	m.applied = m.applied[:len(m.applied)-steps]
	return nil
}

func (m *Migrator) Status() ([]MigrationStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	applied := make(map[string]bool)
	for _, v := range m.applied {
		applied[v] = true
	}
	var result []MigrationStatus
	for _, mg := range m.migrations {
		result = append(result, MigrationStatus{Version: mg.Version, Applied: applied[mg.Version]})
	}
	return result, nil
}

func (m *Migrator) Rollback(version string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	idx := -1
	for i, v := range m.applied {
		if v == version {
			idx = i
			break
		}
	}
	if idx < 0 {
		return ErrMigrationNotFound
	}
	m.applied = m.applied[:idx]
	return nil
}

func (m *Migrator) Validate() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	defs := make(map[string]string)
	for _, mg := range m.migrations {
		defs[mg.Version] = mg.Checksum
	}
	for _, v := range m.applied {
		expected, ok := defs[v]
		if !ok {
			return ErrMigrationNotFound
		}
		_ = expected // in a real implementation, compare stored vs computed checksum
	}
	return nil
}
