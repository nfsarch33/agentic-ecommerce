// Package activityhttp provides a configurable per-call HTTP timeout
// for Temporal activity functions that make outbound HTTP calls
// (carrier APIs, omniparser-bridge, LLM providers, channel adapters).
//
// v4.1.0 IC-7: wraps every outbound HTTP call with
// context.WithTimeout so a hung upstream cannot block a Temporal
// activity indefinitely.
package activityhttp

import (
	"context"
	"os"
	"strconv"
	"time"
)

const (
	envKey         = "EC_ACTIVITY_HTTP_TIMEOUT_SECONDS"
	defaultTimeout = 30 * time.Second
)

// Timeout returns the activity HTTP timeout derived from the
// EC_ACTIVITY_HTTP_TIMEOUT_SECONDS env var, falling back to 30s.
func Timeout() time.Duration {
	if v := os.Getenv(envKey); v != "" {
		if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
			return time.Duration(secs) * time.Second
		}
	}
	return defaultTimeout
}

// WithTimeout derives a child context from ctx with the configured
// activity HTTP timeout. Callers must defer cancel().
func WithTimeout(ctx context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(ctx, Timeout())
}
