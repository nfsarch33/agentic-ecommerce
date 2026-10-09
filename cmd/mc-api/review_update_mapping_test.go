package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/publishgate"
	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"

	ecworkflow "github.com/nfsarch33/agentic-ecommerce/internal/workflow"
)

// fakeUpdateHandle surfaces a configurable Get error (or result), standing
// in for the handle UpdateWorkflow returns.
type fakeUpdateHandle struct {
	getErr error
	res    ecworkflow.ReviewUpdateResult
}

func (h *fakeUpdateHandle) WorkflowID() string { return "wf-123" }
func (h *fakeUpdateHandle) RunID() string      { return "run-1" }
func (h *fakeUpdateHandle) UpdateID() string   { return "upd-1" }
func (h *fakeUpdateHandle) Get(_ context.Context, valuePtr interface{}) error {
	if h.getErr != nil {
		return h.getErr
	}
	raw, err := json.Marshal(h.res)
	if err != nil {
		return err
	}
	return json.Unmarshal(raw, valuePtr)
}

// fakeApprovalStore answers ApprovalFor from a map; the embedded interface
// covers the rest of publishgate.Store (unused here).
type fakeApprovalStore struct {
	publishgate.Store
	byWorkflow map[string]publishgate.Decision
}

func (f fakeApprovalStore) ApprovalFor(_ context.Context, workflowID string) (publishgate.Decision, error) {
	d, ok := f.byWorkflow[workflowID]
	if !ok {
		return publishgate.Decision{}, publishgate.ErrNoApproval
	}
	return d, nil
}

func postReview(srv *server) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/api/v1/workflows/wf-123/signals/review",
		bytes.NewBufferString(`{"approved":true,"reviewer":"lead@example.com"}`))
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	return rec
}

// TestReviewUpdateTypedRejectionMaps409WithStoredDecision pins review
// blocker 6: ONLY the typed ReviewAlreadyDecided refusal maps to 409, and
// the reported decision comes from the approval store, not the error
// message. Mutants: mapping any Get error to 409, or restoring
// decisionStringFromErr (message scraping), both fail this test — a store
// recording approved=true would be reported as rejected.
func TestReviewUpdateTypedRejectionMaps409WithStoredDecision(t *testing.T) {
	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{
		describe:     describeOfWorkflow("wf-123"),
		updateHandle: &fakeUpdateHandle{getErr: temporal.NewNonRetryableApplicationError("already decided", "ReviewAlreadyDecided", nil)},
	}
	srv.approvals = fakeApprovalStore{byWorkflow: map[string]publishgate.Decision{
		"wf-123": {WorkflowID: "wf-123", Approved: true, Actor: "lead@example.com"},
	}}

	rec := postReview(srv)
	if rec.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error    string `json:"error"`
		Decision string `json:"decision"`
		Reviewer string `json:"reviewer"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "already_decided" {
		t.Fatalf("error = %q, want already_decided", body.Error)
	}
	if body.Decision != "approved" || body.Reviewer != "lead@example.com" {
		t.Fatalf("decision/reviewer = %q/%q, want approved/lead@example.com (from the STORE, not the message)", body.Decision, body.Reviewer)
	}
}

// TestReviewUpdateOtherErrorsMap502 pins the second half of blocker 6: a
// Get failure that is NOT the typed refusal (timeout, cancelled context,
// worker failure) must surface as 502, never as a fake 409 decision.
// Mutant: restoring the catch-all 409 mapping fails this test.
func TestReviewUpdateOtherErrorsMap502(t *testing.T) {
	srv, _ := testServer(t)
	srv.workflowClient = &fakeTemporalWorkflowClient{
		describe:     describeOfWorkflow("wf-123"),
		updateHandle: &fakeUpdateHandle{getErr: context.DeadlineExceeded},
	}
	srv.approvals = fakeApprovalStore{byWorkflow: map[string]publishgate.Decision{
		"wf-123": {WorkflowID: "wf-123", Approved: true, Actor: "lead@example.com"},
	}}

	rec := postReview(srv)
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502; body=%s", rec.Code, rec.Body.String())
	}
	var body struct {
		Error string `json:"error"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Error != "workflow_update_failed" {
		t.Fatalf("error = %q, want workflow_update_failed", body.Error)
	}
}

// TestIsReviewAlreadyDecidedTypedOnly is the unit core of the mapping: only
// ApplicationError with type ReviewAlreadyDecided counts. Mutant: matching
// any ApplicationError (dropping the Type check) fails the plain-error case.
func TestIsReviewAlreadyDecidedTypedOnly(t *testing.T) {
	if isReviewAlreadyDecided(nil) {
		t.Fatal("nil error must not be already-decided")
	}
	if isReviewAlreadyDecided(context.DeadlineExceeded) {
		t.Fatal("deadline exceeded must not be already-decided")
	}
	if isReviewAlreadyDecided(errors.New("already decided")) {
		t.Fatal("a plain error with matching TEXT must not count; only the typed error")
	}
	other := temporal.NewNonRetryableApplicationError("boom", "SomethingElse", nil)
	if isReviewAlreadyDecided(other) {
		t.Fatal("a different ApplicationError type must not count")
	}
	if !isReviewAlreadyDecided(temporal.NewNonRetryableApplicationError("already decided", "ReviewAlreadyDecided", nil)) {
		t.Fatal("the typed ReviewAlreadyDecided error must count")
	}
	// The handle wires through to the client interface unchanged.
	var _ client.WorkflowUpdateHandle = (*fakeUpdateHandle)(nil)
}
