package costledger

import "context"

// Attrs carries the job/tenant/action attribution for ledger rows, set
// where a job runs (workflow activities) and read at the model-call seam.
type Attrs struct {
	JobID    string
	TenantID string
	Action   string
}

type attrsKey struct{}

// WithAttrs returns a context carrying ledger attribution.
func WithAttrs(ctx context.Context, a Attrs) context.Context {
	return context.WithValue(ctx, attrsKey{}, a)
}

// AttrFrom returns the attribution on the context (zero value when unset).
func AttrFrom(ctx context.Context) Attrs {
	if ctx == nil {
		return Attrs{}
	}
	a, _ := ctx.Value(attrsKey{}).(Attrs)
	return a
}
