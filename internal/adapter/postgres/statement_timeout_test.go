package postgres_test

import (
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/postgres"
)

func TestLoadStatementTimeoutConfigDefaults(t *testing.T) {
	t.Setenv("EC_PG_READ_TIMEOUT_MS", "")
	t.Setenv("EC_PG_WRITE_TIMEOUT_MS", "")
	cfg := postgres.LoadStatementTimeoutConfig()
	if cfg.ReadMS != 30_000 {
		t.Fatalf("ReadMS = %d, want 30000", cfg.ReadMS)
	}
	if cfg.WriteMS != 60_000 {
		t.Fatalf("WriteMS = %d, want 60000", cfg.WriteMS)
	}
}

func TestLoadStatementTimeoutConfigFromEnv(t *testing.T) {
	t.Setenv("EC_PG_READ_TIMEOUT_MS", "5000")
	t.Setenv("EC_PG_WRITE_TIMEOUT_MS", "10000")
	cfg := postgres.LoadStatementTimeoutConfig()
	if cfg.ReadMS != 5000 {
		t.Fatalf("ReadMS = %d, want 5000", cfg.ReadMS)
	}
	if cfg.WriteMS != 10000 {
		t.Fatalf("WriteMS = %d, want 10000", cfg.WriteMS)
	}
}

func TestLoadStatementTimeoutConfigInvalidFallsBack(t *testing.T) {
	t.Setenv("EC_PG_READ_TIMEOUT_MS", "abc")
	t.Setenv("EC_PG_WRITE_TIMEOUT_MS", "-1")
	cfg := postgres.LoadStatementTimeoutConfig()
	if cfg.ReadMS != 30_000 {
		t.Fatalf("ReadMS = %d, want 30000 (fallback)", cfg.ReadMS)
	}
	if cfg.WriteMS != 60_000 {
		t.Fatalf("WriteMS = %d, want 60000 (fallback)", cfg.WriteMS)
	}
}
