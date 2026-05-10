// Package redis provides configurable connection pool settings for
// Redis clients. v4.1.1 IC-9 exposes pool sizing via env vars so
// operators can tune without recompilation.
package redis

import (
	"os"
	"strconv"
	"time"
)

const (
	envPoolSize       = "EC_REDIS_POOL_SIZE"
	envMinIdleConns   = "EC_REDIS_MIN_IDLE_CONNS"
	envMaxConnAgeSecs = "EC_REDIS_MAX_CONN_AGE_SECONDS"

	defaultPoolSize     = 10
	defaultMinIdleConns = 2
	defaultMaxConnAge   = 30 * time.Minute
)

// PoolConfig holds Redis connection pool settings. Each field has
// an env-var source and a sensible default.
type PoolConfig struct {
	PoolSize     int
	MinIdleConns int
	MaxConnAge   time.Duration
}

// LoadPoolConfig reads pool settings from environment variables,
// falling back to defaults when values are absent or invalid.
func LoadPoolConfig() PoolConfig {
	return PoolConfig{
		PoolSize:     envIntDefault(envPoolSize, defaultPoolSize),
		MinIdleConns: envIntDefault(envMinIdleConns, defaultMinIdleConns),
		MaxConnAge:   envDuration(envMaxConnAgeSecs, defaultMaxConnAge),
	}
}

func envIntDefault(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

func envDuration(key string, fallback time.Duration) time.Duration {
	if v := os.Getenv(key); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return fallback
}
