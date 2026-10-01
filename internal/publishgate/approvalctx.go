package publishgate

import (
	"errors"
	"fmt"
	"net"

	"net/url"
)

// ErrStoreUnreachable marks a store-side connection failure (the counting
// proxy is down, so NOTHING left the worker — 0 store requests). The publish
// workflow maps it to needs_human instead of a failed publish: the write
// never happened, and the retry after the proxy returns is safe because the
// ledger short-circuits completed keys.
var ErrStoreUnreachable = errors.New("store unreachable through the counting proxy")

// classifyRemoteErr wraps connection-level failures as ErrStoreUnreachable.
// A dial error is the proxy being stopped; an HTTP status error is a normal
// store response and stays unclassified.
func classifyRemoteErr(err error) error {
	if err == nil {
		return nil
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && (opErr.Op == "dial" || opErr.Op == "read") {
		return fmt.Errorf("%w: %v", ErrStoreUnreachable, err)
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) && urlErr.Err != nil {
		var inner *net.OpError
		if errors.As(urlErr.Err, &inner) && inner.Op == "dial" {
			return fmt.Errorf("%w: %v", ErrStoreUnreachable, err)
		}
	}
	return err
}
