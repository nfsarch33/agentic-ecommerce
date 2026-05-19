package db_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/db"
)

func TestMigrator_UpAppliesPending(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	m.Add("001", "CREATE TABLE users (id INT)")
	m.Add("002", "CREATE TABLE orders (id INT)")
	if err := m.Up(); err != nil {
		t.Fatalf("Up: %v", err)
	}
	status, _ := m.Status()
	for _, s := range status {
		if !s.Applied {
			t.Fatalf("expected all applied, %s is not", s.Version)
		}
	}
}

func TestMigrator_DownRollsBackN(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	m.Add("001", "sql1")
	m.Add("002", "sql2")
	m.Add("003", "sql3")
	m.Up()
	if err := m.Down(2); err != nil {
		t.Fatalf("Down: %v", err)
	}
	status, _ := m.Status()
	applied := 0
	for _, s := range status {
		if s.Applied {
			applied++
		}
	}
	if applied != 1 {
		t.Fatalf("expected 1 applied after down(2), got %d", applied)
	}
}

func TestMigrator_StatusListsCorrectly(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	m.Add("001", "sql1")
	m.Add("002", "sql2")
	m.Up()
	m.Down(1)
	status, _ := m.Status()
	if len(status) != 2 {
		t.Fatalf("expected 2 status entries, got %d", len(status))
	}
}

func TestMigrator_RollbackToVersion(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	m.Add("001", "sql1")
	m.Add("002", "sql2")
	m.Add("003", "sql3")
	m.Up()
	if err := m.Rollback("002"); err != nil {
		t.Fatalf("Rollback: %v", err)
	}
	status, _ := m.Status()
	for _, s := range status {
		if s.Version >= "002" && s.Applied {
			t.Fatalf("expected versions >= 002 to be rolled back, but %s is applied", s.Version)
		}
	}
}

func TestMigrator_ValidatePassesClean(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	m.Add("001", "sql1")
	m.Up()
	if err := m.Validate(); err != nil {
		t.Fatalf("Validate: %v", err)
	}
}

func TestMigrator_EmptyMigrationSet(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	if err := m.Up(); err != nil {
		t.Fatalf("Up on empty: %v", err)
	}
}

func TestMigrator_DuplicateVersionError(t *testing.T) {
	t.Parallel()
	m := db.NewMigrator()
	m.Add("001", "sql1")
	if err := m.Add("001", "sql2"); err == nil {
		t.Fatal("expected duplicate version error")
	}
}
