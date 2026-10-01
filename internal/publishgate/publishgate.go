// Package publishgate is the exactly-once WooCommerce publish gate: a
// durable decision ledger (publish_approvals), a keyed write ledger with
// leases (publish_ledger), and the Gate that runs the publish decision
// sequence — approval check (fail closed), ledger claim (GET-by-key short
// circuit, lease), remote lookup by SKU, and write-kind selection — so one
// approved decision yields exactly one remote write per idempotency key.
package publishgate

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/approvalid"
)

// Sentinel errors. ErrLeaseHeld is retryable; the others are terminal.
var (
	ErrNoApproval        = errors.New("publishgate: no approved decision for workflow")
	ErrLeaseHeld         = errors.New("publishgate: ledger lease held by a live attempt")
	ErrAlreadyCompleted  = errors.New("publishgate: key already completed")
	ErrRejected          = errors.New("publishgate: decision was rejected")
	ErrDryRun            = errors.New("publishgate: dry-run mode, nothing written")
	ErrKeyBadDraftID     = errors.New("idemkey: draft id is empty or whitespace")
	ErrKeyBadDraftIDChar = errors.New("idemkey: draft id contains ':'")
	ErrKeyNoFields       = errors.New("idemkey: fields is nil or empty")
)

// Key derives the idempotency key: draftID + ":" + the hex SHA-256 of the
// canonical encoding of fields (encoding/json's map encoding is canonical:
// keys sorted, standard escaping). Vendored from the pending internal/idemkey
// harvest; replaced by that package once it merges.
func Key(draftID string, fields map[string]string) (string, error) {
	if strings.TrimSpace(draftID) == "" {
		return "", ErrKeyBadDraftID
	}
	if strings.Contains(draftID, ":") {
		return "", ErrKeyBadDraftIDChar
	}
	if len(fields) == 0 {
		return "", ErrKeyNoFields
	}
	b, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return draftID + ":" + hex.EncodeToString(sum[:]), nil
}

// Matches reports whether every desired field equals the live copy after
// trimming; extra live fields are ignored; empty desired matches nothing.
// Vendored alongside Key.
func Matches(live, desired map[string]string) bool {
	if len(desired) == 0 {
		return false
	}
	for k, v := range desired {
		have, ok := live[k]
		if !ok {
			return false
		}
		if strings.TrimSpace(have) != strings.TrimSpace(v) {
			return false
		}
	}
	return true
}

// Fingerprint is the short hex of the canonical fields, used in workflow
// ids so two starts of the same content converge.
func Fingerprint(fields map[string]string) (string, error) {
	b, err := json.Marshal(fields)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:12]), nil
}

// Decision is a recorded approval or rejection.
type Decision struct {
	WorkflowID string
	TenantID   string
	ProductID  string
	Approved   bool
	Actor      string
	Reason     string
	UpdateID   string
}

// WriteResult reports what the gate did for one publish.
type WriteResult struct {
	WriteKind string // create | update | none | dry_run
	RemoteID  string
	Status    string // completed | dry_run
}

// Store is the durable state the gate needs: the approvals table and the
// ledger. Implementations must be safe for concurrent use.
type Store interface {
	// RecordApproval inserts the decision; a conflicting workflow_id with a
	// different decision returns the stored decision and inserted=false.
	RecordApproval(ctx context.Context, d Decision) (stored Decision, inserted bool, err error)
	// ApprovalFor returns the decision recorded for the workflow, or
	// ErrNoApproval when none exists.
	ApprovalFor(ctx context.Context, workflowID string) (Decision, error)
	// Claim inserts or claims the ledger row for idemKey. Behaviours:
	//   - row completed        -> ErrAlreadyCompleted
	//   - row failed           -> reclaimed (retry)
	//   - lease live (other)   -> ErrLeaseHeld
	//   - lease free/expired   -> claimed: attempts+1, lease_owner/lease_until set
	Claim(ctx context.Context, idemKey, tenantID, workflowID, productID, sku, fingerprint, owner string, leaseTTL time.Duration) error
	// Complete marks the row done with the observed write kind and remote id.
	Complete(ctx context.Context, idemKey, status, writeKind, remoteID string) error
	// Fail marks the row failed and releases the lease.
	Fail(ctx context.Context, idemKey string) error
}

// RemoteProduct is the store-side view the gate needs to decide the write
// kind: the remote id and the live fields for matching.
type RemoteProduct struct {
	ID     string
	Fields map[string]string
}

// WooCommerce is the port the gate drives the store through. FindBySKU must
// return (nil, nil) when the SKU does not exist. Update and Create perform
// the write; Update's returned RemoteProduct reflects the written state.
type WooCommerce interface {
	FindBySKU(ctx context.Context, sku string) (*RemoteProduct, error)
	Update(ctx context.Context, remoteID string, fields map[string]string) (*RemoteProduct, error)
	Create(ctx context.Context, fields map[string]string) (*RemoteProduct, error)
}

// Gate implements the publish decision sequence (design D3-D5).
type Gate struct {
	Store    Store
	Remote   WooCommerce
	DryRun   bool
	LeaseTTL time.Duration
	Owner    string
}

// PublishRequest is one gated publish attempt for an approved workflow.
type PublishRequest struct {
	WorkflowID string
	TenantID   string
	ProductID  string
	SKU        string
	Fields     map[string]string
}

// Publish runs the gate. The returned WriteResult's WriteKind is exactly
// one of create|update|none|dry_run; ErrAlreadyCompleted from Claim
// surfaces as a none-result (idempotent success), matching the design's
// completed-short-circuit.
func (g *Gate) Publish(ctx context.Context, req PublishRequest) (WriteResult, error) {
	dec, err := g.Store.ApprovalFor(ctx, req.WorkflowID)
	if errors.Is(err, ErrNoApproval) {
		return WriteResult{}, fmt.Errorf("publish refuses to write without a recorded decision: %w", err)
	}
	if err != nil {
		return WriteResult{}, fmt.Errorf("publish approval lookup: %w", err)
	}
	if !dec.Approved {
		return WriteResult{}, ErrRejected
	}

	key, err := Key(req.ProductID, req.Fields)
	if err != nil {
		return WriteResult{}, fmt.Errorf("publish key: %w", err)
	}
	fp, err := Fingerprint(req.Fields)
	if err != nil {
		return WriteResult{}, fmt.Errorf("publish fingerprint: %w", err)
	}
	if g.LeaseTTL == 0 {
		g.LeaseTTL = 60 * time.Second
	}
	if err := g.Store.Claim(ctx, key, req.TenantID, req.WorkflowID, req.ProductID, req.SKU, fp, g.Owner, g.LeaseTTL); err != nil {
		if errors.Is(err, ErrAlreadyCompleted) {
			return WriteResult{WriteKind: "none", Status: "completed"}, nil
		}
		return WriteResult{}, err // ErrLeaseHeld is retryable at the caller
	}

	if g.DryRun {
		if err := g.Store.Complete(ctx, key, "dry_run", "dry_run", ""); err != nil {
			return WriteResult{}, fmt.Errorf("publish dry-run complete: %w", err)
		}
		return WriteResult{WriteKind: "dry_run", Status: "dry_run"}, nil
	}

	// The approval row id rides the context so the adapter's writes reach
	// the counting proxy attributable to this approval (v18900-5).
	ctx = approvalid.With(ctx, req.WorkflowID)
	live, err := g.Remote.FindBySKU(ctx, req.SKU)
	if err != nil {
		_ = g.Store.Fail(ctx, key) // release the lease so a retry can proceed
		return WriteResult{}, fmt.Errorf("publish lookup by sku: %w", classifyRemoteErr(err))
	}
	switch {
	case live != nil && Matches(live.Fields, req.Fields):
		if err := g.Store.Complete(ctx, key, "completed", "none", live.ID); err != nil {
			return WriteResult{}, fmt.Errorf("publish complete(none): %w", err)
		}
		return WriteResult{WriteKind: "none", RemoteID: live.ID, Status: "completed"}, nil
	case live != nil:
		updated, err := g.Remote.Update(ctx, live.ID, req.Fields)
		if err != nil {
			_ = g.Store.Fail(ctx, key)
			return WriteResult{}, fmt.Errorf("publish update: %w", classifyRemoteErr(err))
		}
		if err := g.Store.Complete(ctx, key, "completed", "update", updated.ID); err != nil {
			return WriteResult{}, fmt.Errorf("publish complete(update): %w", err)
		}
		return WriteResult{WriteKind: "update", RemoteID: updated.ID, Status: "completed"}, nil
	default:
		// The SKU is the anchor the next attempt's lookup keys on (D5): it
		// must be part of the write even when the caller's fields omit it.
		createFields := make(map[string]string, len(req.Fields)+1)
		for k, v := range req.Fields {
			createFields[k] = v
		}
		createFields["sku"] = req.SKU
		created, err := g.Remote.Create(ctx, createFields)
		if err != nil {
			_ = g.Store.Fail(ctx, key)
			return WriteResult{}, fmt.Errorf("publish create: %w", classifyRemoteErr(err))
		}
		if err := g.Store.Complete(ctx, key, "completed", "create", created.ID); err != nil {
			return WriteResult{}, fmt.Errorf("publish complete(create): %w", err)
		}
		return WriteResult{WriteKind: "create", RemoteID: created.ID, Status: "completed"}, nil
	}
}

// ErrSQLNoRows adapts sql.ErrNoRows for implementations that wrap it.
var ErrSQLNoRows = sql.ErrNoRows
