package publishgate

import (
	"time"

	"context"
	"errors"
	"fmt"
	"net"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/approvalid"
)

// MUTANT: drop the dial classification in classifyRemoteErr and the proxy
// being down surfaces as a plain failure (the workflow's needs_human branch
// never fires); this goes red.
func TestClassifyRemoteErrMarksDialFailuresUnreachable(t *testing.T) {
	dialErr := &net.OpError{Op: "dial", Net: "tcp", Err: fmt.Errorf("connection refused")}
	wrapped := fmt.Errorf("publish update: %w", dialErr)
	if !errors.Is(classifyRemoteErr(wrapped), ErrStoreUnreachable) {
		t.Fatalf("a dial failure must classify as ErrStoreUnreachable, got %v", classifyRemoteErr(wrapped))
	}
}

// MUTANT: classify HTTP status errors as unreachable and a store 500 would
// route to needs_human instead of a retryable failure; this goes red.
func TestClassifyRemoteErrLeavesHTTPErrorsAlone(t *testing.T) {
	httpErr := errors.New("woocommerce status 500")
	if errors.Is(classifyRemoteErr(httpErr), ErrStoreUnreachable) {
		t.Fatal("a store status error is NOT an unreachable-proxy case")
	}
}

// MUTANT: the gate forgets to thread the approval id into the context and
// the adapter's writes reach the proxy unauditable (403 at the proxy). This
// drives the gate with a recording remote and pins the context value.
func TestGateThreadsApprovalIDIntoRemoteContext(t *testing.T) {
	var seen string
	g := &Gate{
		Store: &stubCtxStore{},
		Remote: remoteFuncs{
			find: func(ctx context.Context, sku string) (*RemoteProduct, error) {
				seen = ApprovalIDStub(ctx)
				return nil, nil // not found -> create path
			},
			create: func(ctx context.Context, fields map[string]string) (*RemoteProduct, error) {
				if seen == "" {
					seen = ApprovalIDStub(ctx)
				}
				return &RemoteProduct{ID: "1", Fields: fields}, nil
			},
		},
		Owner: "test",
	}
	if _, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-approval-1", SKU: "SKU-X", ProductID: "p1", Fields: map[string]string{"title": "t"}}); err != nil {
		t.Fatalf("Publish: %v", err)
	}
	if seen != "wf-approval-1" {
		t.Fatalf("remote saw approval id %q, want wf-approval-1 (the workflow id IS the approval row key)", seen)
	}
}

// ApprovalIDStub reads the approval id via the leaf package (kept out of the
// non-test file to avoid the woocommerce cycle).
func ApprovalIDStub(ctx context.Context) string { return approvalid.From(ctx) }

// remoteFuncs is a Remote built from function fields.
type remoteFuncs struct {
	find   func(ctx context.Context, sku string) (*RemoteProduct, error)
	update func(ctx context.Context, id string, fields map[string]string) (*RemoteProduct, error)
	create func(ctx context.Context, fields map[string]string) (*RemoteProduct, error)
}

func (r remoteFuncs) FindBySKU(ctx context.Context, sku string) (*RemoteProduct, error) {
	return r.find(ctx, sku)
}
func (r remoteFuncs) Update(ctx context.Context, id string, fields map[string]string) (*RemoteProduct, error) {
	return r.update(ctx, id, fields)
}
func (r remoteFuncs) Create(ctx context.Context, fields map[string]string) (*RemoteProduct, error) {
	return r.create(ctx, fields)
}

// stubCtxStore is a minimal in-memory Store for context-threading tests.
type stubCtxStore struct{ completed bool }

func (s *stubCtxStore) RecordApproval(_ context.Context, d Decision) (Decision, bool, error) {
	return d, true, nil
}
func (s *stubCtxStore) ApprovalFor(_ context.Context, _ string) (Decision, error) {
	return Decision{Approved: true}, nil
}
func (s *stubCtxStore) Claim(_ context.Context, _, _, _, _, _, _, _ string, _ time.Duration) error {
	return nil
}
func (s *stubCtxStore) Complete(_ context.Context, _, _, _, _ string) error {
	s.completed = true
	return nil
}
func (s *stubCtxStore) Fail(_ context.Context, _ string) error { return nil }
