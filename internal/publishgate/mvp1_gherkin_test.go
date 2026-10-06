package publishgate

import (
	"context"
	"strings"
	"testing"
)

// The MVP-1 QA Gherkin (the launch plan, Epic 2), automated against the
// same in-memory store and fake remote the gate tests use. One row per
// scenario; the Gherkin text each row implements is quoted above it so
// the plan and the suite cannot drift silently.
//
// MUTANT (scenario 2): make Gate.Publish skip the Claim (idempotency)
// and the concurrent-approval row goes red — two REST writes land.
// MUTANT (scenario 3): make Publish skip the live-fingerprint check and
// the crash-restart row goes red — the retried publish writes twice.

// Scenario 1: "A drafted product description is never published without
// approval / When the publish worker runs / Then the WooCommerce product
// is unchanged / And the audit log records 'skipped: not approved'."
func TestMVP1_Gherkin_NoApprovalNeverPublishes(t *testing.T) {
	t.Parallel()
	remote := &fakeRemote{bySKU: map[string]*RemoteProduct{}}
	store := newMemStore() // NO approval row for wf-none
	g := &Gate{Store: store, Remote: remote, Owner: "w1"}

	_, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-none", TenantID: "t1", ProductID: "p1", SKU: "S-1", Fields: map[string]string{"name": "draft"}})
	if err == nil {
		t.Fatal("a publish without an approval row must fail")
	}
	if remote.posts+remote.puts != 0 {
		t.Fatalf("the remote must stay untouched without approval: posts=%d puts=%d", remote.posts, remote.puts)
	}
	// The audit shape: the refusal names the reason the operator greps.
	if want := "publish refuses to write without a recorded decision"; !strings.Contains(err.Error(), want) {
		t.Fatalf("the refusal must carry the audit wording, got %q", err.Error())
	}
}

// Scenario 2: "Double approval publishes exactly once / When two
// approvals for the same draft arrive concurrently / Then exactly one
// REST update is sent with the draft's idempotency key / And the second
// approval returns 'already approved'."
func TestMVP1_Gherkin_DoubleApprovalPublishesOnce(t *testing.T) {
	t.Parallel()
	store := approvedStore(t) // one approval for wf-1
	remote := &fakeRemote{bySKU: map[string]*RemoteProduct{}}
	g := &Gate{Store: store, Remote: remote, Owner: "w1"}

	// The second DECISION is first-decision-wins: the store refuses it.
	_, firstWins, err := store.RecordApproval(context.Background(), Decision{
		WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", Approved: false, Actor: "second", UpdateID: "k2",
	})
	if err != nil {
		t.Fatal(err)
	}
	if firstWins {
		t.Fatal("a second decision on the same draft must not win (the caller maps this to already approved)")
	}

	// Both racing approvals each drive a publish. The lease is
	// deliberately same-owner re-entrant (PG: OR lease_owner = $2 — one
	// worker process may reclaim its own key), so the exactly-once
	// invariant is the COMPLETED LEDGER: the first publish completes the
	// ledger row, and the second returns at the ErrAlreadyCompleted
	// short-circuit before it ever reaches the remote. Two sequential
	// publishes, one REST write — the fingerprint check is the crash
	// path's guard (scenario 3), not this one's.
	for i := 0; i < 2; i++ {
		if _, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S-2", Fields: map[string]string{"name": "once"}}); err != nil {
			t.Fatalf("publish %d: %v", i+1, err)
		}
	}
	if remote.posts+remote.puts != 1 {
		t.Fatalf("exactly one REST write may land (the second returns at the completed-ledger short-circuit), got posts=%d puts=%d", remote.posts, remote.puts)
	}
}

// Scenario 3: "A crash during publish does not duplicate the change /
// Given an approved draft whose REST update was sent / When the worker
// is killed before recording the result and restarts / Then the worker
// checks the live product before retrying / And no second update is sent
// when the change is already present."
func TestMVP1_Gherkin_CrashAfterSendBeforeLedgerWritesOnce(t *testing.T) {
	t.Parallel()
	store := approvedStore(t)
	// The crash shape: the first publish SENDS the create but never
	// records the ledger row — simulate by publishing with a remote whose
	// Create succeeds, then REWINDING the ledger claim as if the process
	// died before Complete.
	remote := &fakeRemote{bySKU: map[string]*RemoteProduct{}}
	g := &Gate{Store: store, Remote: remote, Owner: "w1"}

	if _, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S-3", Fields: map[string]string{"name": "v1"}}); err != nil {
		t.Fatal(err)
	}
	sent := remote.posts + remote.puts
	if sent != 1 {
		t.Fatalf("setup: the first publish must send exactly one write, got %d", sent)
	}

	// Kill-before-record: the dead process never wrote the ledger row;
	// the in-memory Fail is the crash proxy and MUST succeed.
	if err := store.Fail(context.Background(), mustKey(t, "p1", map[string]string{"name": "v1"})); err != nil {
		t.Fatal(err)
	}
	remote.bySKU["S-3"] = &RemoteProduct{ID: "wp-100", Fields: map[string]string{"name": "v1"}}

	res, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S-3", Fields: map[string]string{"name": "v1"}})
	if err != nil {
		t.Fatalf("the restarted publish must succeed via the live check: %v", err)
	}
	if remote.posts+remote.puts != sent {
		t.Fatalf("a restart finding the change already live must not write again: sent=%d total=%d", sent, remote.posts+remote.puts)
	}
	if res.WriteKind == "create" || res.WriteKind == "update" {
		t.Fatalf("the no-op result must not report a write, got %q", res.WriteKind)
	}
}

// Scenario 4: "Retries stop and alert / Given the model endpoint fails
// every request / When a draft job is retried / Then it stops after 3
// attempts with backoff / And the cost ledger shows the attempts / And an
// alert is raised the same day." The workflow-level retry rows live in
// internal/workflow (Retries*Activity*); this row pins the GATE side —
// a failing remote releases the lease so the NEXT attempt can run, and
// Fail is what the ledger reads for the attempt count.
func TestMVP1_Gherkin_FailingRemoteReleasesForRetry(t *testing.T) {
	t.Parallel()
	store := approvedStore(t)
	remote := &fakeRemote{bySKU: map[string]*RemoteProduct{}, failSKU: true}
	g := &Gate{Store: store, Remote: remote, Owner: "w1"}

	for attempt := 1; attempt <= 3; attempt++ {
		if _, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S-4", Fields: map[string]string{"name": "x"}}); err == nil {
			t.Fatalf("attempt %d must fail while the store is down", attempt)
		}
	}
	// The store heals: the fourth attempt (the alerting/stop policy is
	// the workflow's, pinned there) can proceed because every failure
	// released the lease instead of wedging it.
	remote.failSKU = false
	if _, err := g.Publish(context.Background(), PublishRequest{WorkflowID: "wf-1", TenantID: "t1", ProductID: "p1", SKU: "S-4", Fields: map[string]string{"name": "x"}}); err != nil {
		t.Fatalf("after the store heals a retry must proceed (failures must release the lease): %v", err)
	}
}

func mustKey(t *testing.T, productID string, fields map[string]string) string {
	t.Helper()
	k, err := Key(productID, fields)
	if err != nil {
		t.Fatal(err)
	}
	return k
}
