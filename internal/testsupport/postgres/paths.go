package testsupportpg

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
)

// ResolveMigrationDir locates the migrations/ directory relative to
// THIS file using runtime.Caller so callers from any depth (e.g.
// internal/adapter/postgres or internal/rag) end up with the same
// canonical path. Exposed as a public helper so non-Docker callers
// (lint scripts, schema fixture generators) can reuse the same lookup.
func ResolveMigrationDir() (string, error) {
	_, here, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("runtime.Caller failed")
	}
	// paths.go lives at <repo>/internal/testsupport/postgres/paths.go;
	// migrations/ lives at <repo>/migrations/.
	dir := filepath.Join(filepath.Dir(here), "..", "..", "..", "migrations")
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}
