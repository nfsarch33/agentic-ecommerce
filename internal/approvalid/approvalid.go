// Package approvalid carries the approval row id from the publish gate to
// the store adapter (v18900-5). It is a leaf: the gate, the WooCommerce
// adapter and the counting proxy all reference it without cycles.
package approvalid

import "context"

// Header is the HTTP header the counting proxy requires on every write.
const Header = "X-Approval-Id"

type key struct{}

// With returns a context carrying the approval row id.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, key{}, id)
}

// From returns the approval id on the context, or "".
func From(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(key{}).(string)
	return id
}
