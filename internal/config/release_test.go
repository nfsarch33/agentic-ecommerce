package config_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/config"
)

func TestRelease_ParseVersion(t *testing.T) {
	t.Parallel()
	v, err := config.ParseVersion("v10.2.3")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	if v.Major != 10 || v.Minor != 2 || v.Patch != 3 {
		t.Fatalf("unexpected version: %+v", v)
	}
}

func TestRelease_ParseVersionWithPreRelease(t *testing.T) {
	t.Parallel()
	v, err := config.ParseVersion("v1.0.0-rc.1")
	if err != nil {
		t.Fatalf("ParseVersion: %v", err)
	}
	if v.PreRelease != "rc.1" {
		t.Fatalf("expected rc.1, got %s", v.PreRelease)
	}
}

func TestRelease_Changelog(t *testing.T) {
	t.Parallel()
	entries, err := config.Changelog("v8.0.0", "v10.0.0")
	if err != nil {
		t.Fatalf("Changelog: %v", err)
	}
	if len(entries) == 0 {
		t.Fatal("expected changelog entries")
	}
}

func TestRelease_CompatibilityMatrix(t *testing.T) {
	t.Parallel()
	m := config.CompatibilityMatrix()
	if len(m.APIVersions) == 0 {
		t.Fatal("expected API versions in compatibility matrix")
	}
}

func TestRelease_InvalidVersionError(t *testing.T) {
	t.Parallel()
	if _, err := config.ParseVersion("not-a-version"); err == nil {
		t.Fatal("expected invalid version error")
	}
}
