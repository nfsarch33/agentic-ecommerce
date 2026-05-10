package postgres

import (
	"context"
	"fmt"
	"os"
	"strconv"
)

const (
	envReadTimeoutMS  = "EC_PG_READ_TIMEOUT_MS"
	envWriteTimeoutMS = "EC_PG_WRITE_TIMEOUT_MS"
	defaultReadMS     = 30_000
	defaultWriteMS    = 60_000
)

// StatementTimeoutConfig holds per-query-type statement_timeout values.
type StatementTimeoutConfig struct {
	ReadMS  int
	WriteMS int
}

// LoadStatementTimeoutConfig reads EC_PG_READ_TIMEOUT_MS and
// EC_PG_WRITE_TIMEOUT_MS from the environment. Falls back to 30s
// read / 60s write if the env vars are absent or unparseable.
func LoadStatementTimeoutConfig() StatementTimeoutConfig {
	return StatementTimeoutConfig{
		ReadMS:  envInt(envReadTimeoutMS, defaultReadMS),
		WriteMS: envInt(envWriteTimeoutMS, defaultWriteMS),
	}
}

func envInt(key string, fallback int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			return n
		}
	}
	return fallback
}

// SetStatementTimeout applies SET LOCAL statement_timeout on the
// given connection. Uses SET LOCAL so the GUC resets at transaction
// end and does not leak to other queries on the same pooled
// connection.
func SetStatementTimeout(ctx context.Context, conn TenantConn, ms int) error {
	sql := fmt.Sprintf("SET LOCAL statement_timeout = '%dms'", ms)
	if _, err := conn.Exec(ctx, sql); err != nil {
		return fmt.Errorf("set statement_timeout: %w", err)
	}
	return nil
}

// SetReadTimeout is a convenience wrapper that applies the read
// timeout from the config.
func SetReadTimeout(ctx context.Context, conn TenantConn, cfg StatementTimeoutConfig) error {
	return SetStatementTimeout(ctx, conn, cfg.ReadMS)
}

// SetWriteTimeout is a convenience wrapper that applies the write
// timeout from the config.
func SetWriteTimeout(ctx context.Context, conn TenantConn, cfg StatementTimeoutConfig) error {
	return SetStatementTimeout(ctx, conn, cfg.WriteMS)
}
