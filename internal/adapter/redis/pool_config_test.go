package redis_test

import (
	"testing"
	"time"

	rediscfg "github.com/nfsarch33/helixon-ec/internal/adapter/redis"
)

func TestLoadPoolConfigDefaults(t *testing.T) {
	t.Setenv("EC_REDIS_POOL_SIZE", "")
	t.Setenv("EC_REDIS_MIN_IDLE_CONNS", "")
	t.Setenv("EC_REDIS_MAX_CONN_AGE_SECONDS", "")
	cfg := rediscfg.LoadPoolConfig()
	if cfg.PoolSize != 10 {
		t.Fatalf("PoolSize = %d, want 10", cfg.PoolSize)
	}
	if cfg.MinIdleConns != 2 {
		t.Fatalf("MinIdleConns = %d, want 2", cfg.MinIdleConns)
	}
	if cfg.MaxConnAge != 30*time.Minute {
		t.Fatalf("MaxConnAge = %v, want 30m", cfg.MaxConnAge)
	}
}

func TestLoadPoolConfigFromEnv(t *testing.T) {
	t.Setenv("EC_REDIS_POOL_SIZE", "25")
	t.Setenv("EC_REDIS_MIN_IDLE_CONNS", "5")
	t.Setenv("EC_REDIS_MAX_CONN_AGE_SECONDS", "600")
	cfg := rediscfg.LoadPoolConfig()
	if cfg.PoolSize != 25 {
		t.Fatalf("PoolSize = %d, want 25", cfg.PoolSize)
	}
	if cfg.MinIdleConns != 5 {
		t.Fatalf("MinIdleConns = %d, want 5", cfg.MinIdleConns)
	}
	if cfg.MaxConnAge != 600*time.Second {
		t.Fatalf("MaxConnAge = %v, want 10m", cfg.MaxConnAge)
	}
}

func TestLoadPoolConfigInvalidFallsBack(t *testing.T) {
	t.Setenv("EC_REDIS_POOL_SIZE", "abc")
	t.Setenv("EC_REDIS_MIN_IDLE_CONNS", "-1")
	t.Setenv("EC_REDIS_MAX_CONN_AGE_SECONDS", "")
	cfg := rediscfg.LoadPoolConfig()
	if cfg.PoolSize != 10 {
		t.Fatalf("PoolSize = %d, want 10 (fallback)", cfg.PoolSize)
	}
	if cfg.MinIdleConns != 2 {
		t.Fatalf("MinIdleConns = %d, want 2 (fallback)", cfg.MinIdleConns)
	}
}
