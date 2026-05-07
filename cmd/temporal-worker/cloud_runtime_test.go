package main

import (
	"testing"
	"time"
)

func TestTemporalDatabasePoolConfigFromEnvAppliesCloudTuning(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONNS", "24")
	t.Setenv("ECOMMERCE_DB_POOL_MIN_CONNS", "6")
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONN_LIFETIME", "40m")
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONN_IDLE_TIME", "6m")
	t.Setenv("ECOMMERCE_DB_CONNECT_TIMEOUT", "4s")

	cfg, err := temporalDatabasePoolConfigFromEnv("postgres://ecommerce:secret@db.internal:5432/ecommerce?sslmode=require")
	if err != nil {
		t.Fatalf("temporalDatabasePoolConfigFromEnv: %v", err)
	}
	if cfg.MaxConns != 24 || cfg.MinConns != 6 {
		t.Fatalf("pool conns = max:%d min:%d, want 24/6", cfg.MaxConns, cfg.MinConns)
	}
	if cfg.MaxConnLifetime != 40*time.Minute || cfg.MaxConnIdleTime != 6*time.Minute {
		t.Fatalf("pool lifetimes = %s/%s, want 40m/6m", cfg.MaxConnLifetime, cfg.MaxConnIdleTime)
	}
	if cfg.ConnConfig.ConnectTimeout != 4*time.Second {
		t.Fatalf("connect timeout = %s, want 4s", cfg.ConnConfig.ConnectTimeout)
	}
}

func TestTemporalDatabasePoolConfigFromEnvUsesSafeFallbacks(t *testing.T) {
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONNS", "not-an-int")
	t.Setenv("ECOMMERCE_DB_POOL_MIN_CONNS", "99")
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONN_LIFETIME", "not-a-duration")
	t.Setenv("ECOMMERCE_DB_POOL_MAX_CONN_IDLE_TIME", "0s")
	t.Setenv("ECOMMERCE_DB_CONNECT_TIMEOUT", "-1s")

	cfg, err := temporalDatabasePoolConfigFromEnv("postgres://ecommerce:secret@db.internal:5432/ecommerce?sslmode=require")
	if err != nil {
		t.Fatalf("temporalDatabasePoolConfigFromEnv: %v", err)
	}
	if cfg.MaxConns != 10 || cfg.MinConns != 10 {
		t.Fatalf("fallback conns = max:%d min:%d, want 10/10", cfg.MaxConns, cfg.MinConns)
	}
	if cfg.MaxConnLifetime != 30*time.Minute || cfg.MaxConnIdleTime != 5*time.Minute {
		t.Fatalf("fallback lifetimes = %s/%s, want 30m/5m", cfg.MaxConnLifetime, cfg.MaxConnIdleTime)
	}
	if cfg.ConnConfig.ConnectTimeout != 5*time.Second {
		t.Fatalf("fallback connect timeout = %s, want 5s", cfg.ConnConfig.ConnectTimeout)
	}
}
