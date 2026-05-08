package testsupportpg

import (
	"os"
	"path/filepath"
	"testing"
)

// TestCanonicalMigrationFilesAreOrderedAndUnique checks the ledger that
// every integration_pg test relies on.
func TestCanonicalMigrationFilesAreOrderedAndUnique(t *testing.T) {
	files := CanonicalMigrationFiles()
	if len(files) == 0 {
		t.Fatal("CanonicalMigrationFiles returned empty list")
	}
	seen := make(map[string]struct{}, len(files))
	prev := ""
	for _, f := range files {
		if f == "" {
			t.Fatalf("blank migration file in canonical list")
		}
		if _, dup := seen[f]; dup {
			t.Fatalf("duplicate migration file %q", f)
		}
		seen[f] = struct{}{}
		// Filenames are 4-digit ordinal prefixed; lexicographic order
		// matches application order.
		if prev != "" && f <= prev {
			t.Fatalf("migration files out of order: %q before %q", prev, f)
		}
		prev = f
	}
}

// TestResolveMigrationDirReturnsCanonicalPath verifies the runtime
// caller-based resolution lands on a directory containing every
// canonical migration file. This guards against silent breakage if the
// package path moves.
func TestResolveMigrationDirReturnsCanonicalPath(t *testing.T) {
	dir, err := ResolveMigrationDir()
	if err != nil {
		t.Fatalf("ResolveMigrationDir: %v", err)
	}
	for _, name := range CanonicalMigrationFiles() {
		if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
			t.Fatalf("canonical migration %s missing: %v", name, err)
		}
	}
}
