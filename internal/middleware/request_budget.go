// File scope: v6.2.0 Story 3 per-handler request budget guard.
//
// Wraps the downstream handler with context.WithTimeout so any
// goroutine derived from r.Context() is forced to release when the
// budget fires. Reuses the existing memcap middleware shape so the
// composition root can chain RequestBudget(...)(MemCap(...)(handler))
// without surprising semantics.
//
// Decomposition (HARD GATE): cyclomatic stays at 3.
package middleware

import (
	"context"
	"net/http"
	"time"
)

// RequestBudgetConfig governs the per-request handler timeout.
type RequestBudgetConfig struct {
	// Default budget applied when the per-tenant override is absent.
	// Default <= 0 disables the budget entirely (preserves the
	// pre-v6.2.0 behaviour for routes that opt out explicitly).
	Default time.Duration

	// TenantOverride returns the per-tenant budget when (ok). Useful
	// for slow-path admin routes that need a longer ceiling.
	TenantOverride func(r *http.Request) (time.Duration, bool)
}

// RequestBudget returns the middleware that bounds every downstream
// handler with context.WithTimeout. The cancel func always runs in a
// defer so leaked goroutines spawned via r.Context() observe Done.
func RequestBudget(cfg RequestBudgetConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			budget := cfg.Default
			if cfg.TenantOverride != nil {
				if v, ok := cfg.TenantOverride(r); ok {
					budget = v
				}
			}
			if budget <= 0 {
				next.ServeHTTP(w, r)
				return
			}
			ctx, cancel := context.WithTimeout(r.Context(), budget)
			defer cancel()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
