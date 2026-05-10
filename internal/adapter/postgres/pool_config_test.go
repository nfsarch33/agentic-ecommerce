package postgres

import (
	"testing"
	"time"
)

func TestLoadPGPoolConfigDefaults(t *testing.T) {
	t.Setenv("EC_PG_MAX_OPEN_CONNS", "")
	t.Setenv("EC_PG_MAX_IDLE_CONNS", "")
	t.Setenv("EC_PG_CONN_MAX_LIFETIME_MINUTES", "")
	t.Setenv("EC_PG_CONN_MAX_IDLE_TIME_MINUTES", "")

	cfg := LoadPGPoolConfig()

	if cfg.MaxOpenConns != 25 {
		t.Fatalf("MaxOpenConns = %d, want 25", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 10 {
		t.Fatalf("MaxIdleConns = %d, want 10", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 30*time.Minute {
		t.Fatalf("ConnMaxLifetime = %v, want 30m", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 5*time.Minute {
		t.Fatalf("ConnMaxIdleTime = %v, want 5m", cfg.ConnMaxIdleTime)
	}
}

func TestLoadPGPoolConfigFromEnv(t *testing.T) {
	t.Setenv("EC_PG_MAX_OPEN_CONNS", "50")
	t.Setenv("EC_PG_MAX_IDLE_CONNS", "20")
	t.Setenv("EC_PG_CONN_MAX_LIFETIME_MINUTES", "60")
	t.Setenv("EC_PG_CONN_MAX_IDLE_TIME_MINUTES", "10")

	cfg := LoadPGPoolConfig()

	if cfg.MaxOpenConns != 50 {
		t.Fatalf("MaxOpenConns = %d, want 50", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 20 {
		t.Fatalf("MaxIdleConns = %d, want 20", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 60*time.Minute {
		t.Fatalf("ConnMaxLifetime = %v, want 60m", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 10*time.Minute {
		t.Fatalf("ConnMaxIdleTime = %v, want 10m", cfg.ConnMaxIdleTime)
	}
}

func TestLoadPGPoolConfigInvalidFallsBack(t *testing.T) {
	t.Setenv("EC_PG_MAX_OPEN_CONNS", "abc")
	t.Setenv("EC_PG_MAX_IDLE_CONNS", "-1")
	t.Setenv("EC_PG_CONN_MAX_LIFETIME_MINUTES", "0")
	t.Setenv("EC_PG_CONN_MAX_IDLE_TIME_MINUTES", "not-a-number")

	cfg := LoadPGPoolConfig()

	if cfg.MaxOpenConns != 25 {
		t.Fatalf("MaxOpenConns = %d, want 25 (fallback)", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 10 {
		t.Fatalf("MaxIdleConns = %d, want 10 (fallback)", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 30*time.Minute {
		t.Fatalf("ConnMaxLifetime = %v, want 30m (fallback)", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 5*time.Minute {
		t.Fatalf("ConnMaxIdleTime = %v, want 5m (fallback)", cfg.ConnMaxIdleTime)
	}
}

func TestLoadPGPoolConfigPartialOverride(t *testing.T) {
	t.Setenv("EC_PG_MAX_OPEN_CONNS", "100")
	t.Setenv("EC_PG_MAX_IDLE_CONNS", "")
	t.Setenv("EC_PG_CONN_MAX_LIFETIME_MINUTES", "")
	t.Setenv("EC_PG_CONN_MAX_IDLE_TIME_MINUTES", "15")

	cfg := LoadPGPoolConfig()

	if cfg.MaxOpenConns != 100 {
		t.Fatalf("MaxOpenConns = %d, want 100", cfg.MaxOpenConns)
	}
	if cfg.MaxIdleConns != 10 {
		t.Fatalf("MaxIdleConns = %d, want 10 (default)", cfg.MaxIdleConns)
	}
	if cfg.ConnMaxLifetime != 30*time.Minute {
		t.Fatalf("ConnMaxLifetime = %v, want 30m (default)", cfg.ConnMaxLifetime)
	}
	if cfg.ConnMaxIdleTime != 15*time.Minute {
		t.Fatalf("ConnMaxIdleTime = %v, want 15m", cfg.ConnMaxIdleTime)
	}
}
