// Package postgres pool_config exposes Postgres connection pool
// settings via env vars so operators can tune without recompilation.
// Mirrors the v4.1.0 IC-9 Redis pool_config pattern in
// internal/adapter/redis/pool_config.go.
package postgres

import (
	"os"
	"strconv"
	"time"
)

const (
	envMaxOpenConns        = "EC_PG_MAX_OPEN_CONNS"
	envMaxIdleConns        = "EC_PG_MAX_IDLE_CONNS"
	envConnMaxLifetimeMins = "EC_PG_CONN_MAX_LIFETIME_MINUTES"
	envConnMaxIdleTimeMins = "EC_PG_CONN_MAX_IDLE_TIME_MINUTES"

	defaultMaxOpenConns    = 25
	defaultMaxIdleConns    = 10
	defaultConnMaxLifetime = 30 * time.Minute
	defaultConnMaxIdleTime = 5 * time.Minute
)

// PGPoolConfig holds Postgres connection pool settings. Each field
// has an env-var source and a sensible default.
type PGPoolConfig struct {
	MaxOpenConns    int
	MaxIdleConns    int
	ConnMaxLifetime time.Duration
	ConnMaxIdleTime time.Duration
}

// LoadPGPoolConfig reads pool settings from environment variables,
// falling back to defaults when values are absent or invalid.
func LoadPGPoolConfig() PGPoolConfig {
	return PGPoolConfig{
		MaxOpenConns:    pgEnvInt(envMaxOpenConns, defaultMaxOpenConns),
		MaxIdleConns:    pgEnvInt(envMaxIdleConns, defaultMaxIdleConns),
		ConnMaxLifetime: pgEnvDurationMins(envConnMaxLifetimeMins, defaultConnMaxLifetime),
		ConnMaxIdleTime: pgEnvDurationMins(envConnMaxIdleTimeMins, defaultConnMaxIdleTime),
	}
}

func pgEnvInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func pgEnvDurationMins(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if mins, err := strconv.Atoi(v); err == nil && mins > 0 {
			return time.Duration(mins) * time.Minute
		}
	}
	return fallback
}
