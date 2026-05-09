// Package middleware ships net/http middleware shared across the
// mc-api binary's API surface.
//
// MemCap is the per-request memory cap from v2.10.0 Story 3. It
// rejects requests with Content-Length > MaxRequestBytes (configurable
// per tenant) and wraps the body in http.MaxBytesReader as defence in
// depth so streaming/chunked clients cannot bypass the static check.
package middleware

import (
	"fmt"
	"net/http"
)

// MemCapConfig governs the per-request memory cap.
type MemCapConfig struct {
	MaxRequestBytes int64
	TenantOverride  func(r *http.Request) (int64, bool)
}

// MemCap returns the middleware that enforces MaxRequestBytes.
// MaxRequestBytes <= 0 disables the cap (used for tests + opt-out).
func MemCap(cfg MemCapConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			limit := cfg.MaxRequestBytes
			if cfg.TenantOverride != nil {
				if v, ok := cfg.TenantOverride(r); ok {
					limit = v
				}
			}
			if limit <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			if r.ContentLength > limit {
				rejectTooLarge(w, limit, r.ContentLength)
				return
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)
			next.ServeHTTP(w, r)
		})
	}
}

func rejectTooLarge(w http.ResponseWriter, limit, declared int64) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusRequestEntityTooLarge)
	_, _ = fmt.Fprintf(w, `{"error":"request body exceeds limit","limit_bytes":%d,"declared_bytes":%d}`, limit, declared)
}
