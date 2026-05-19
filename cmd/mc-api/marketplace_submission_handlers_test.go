package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/adapter/inmemory"
	"github.com/nfsarch33/helixon-ec/internal/marketplace"
)

func newSubmissionTestServer(t *testing.T) *server {
	t.Helper()
	cat := inmemory.NewMarketplaceCatalog()
	mkSvc, err := marketplace.NewService(marketplace.ServiceConfig{
		Catalog:       cat,
		Installations: inmemory.NewMarketplaceInstallations(),
		Subscriptions: inmemory.NewMarketplaceSubscriptions(),
	})
	if err != nil {
		t.Fatalf("NewService: %v", err)
	}
	subRepo := inmemory.NewMarketplaceSubmissions()
	subSvc, err := marketplace.NewSubmissionService(marketplace.SubmissionServiceConfig{
		Submissions: subRepo,
		Catalog:     mkSvc.Catalog(),
		Clock:       func() string { return "2026-05-09T03:00:00Z" },
	})
	if err != nil {
		t.Fatalf("NewSubmissionService: %v", err)
	}
	return &server{marketplace: mkSvc, marketplaceSubs: subSvc}
}

func tenantReq(method, path string, body any) *http.Request {
	var buf []byte
	if body != nil {
		buf, _ = json.Marshal(body)
	}
	req := httptest.NewRequest(method, path, bytes.NewReader(buf))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	req.Header.Set("Content-Type", "application/json")
	return req
}

func adminReq(method, path string, body any) *http.Request {
	req := tenantReq(method, path, body)
	ctx := context.WithValue(req.Context(), actorContextKey{}, requestActor{Subject: "admin@example.com", TenantID: "tenant-a"})
	return req.WithContext(ctx)
}

func validSubmissionBody() submissionRequest {
	return submissionRequest{
		SubmitterEmail: "vendor@example.com",
		Slug:           "stripe-payments",
		Name:           "Stripe Payments",
		Version:        "1.0.0",
		Vendor:         "Stripe",
		Category:       "payments",
	}
}

func TestSubmissionHandlers_ServiceUnavailable(t *testing.T) {
	t.Parallel()
	srv := &server{}
	rec := httptest.NewRecorder()
	srv.submitMarketplacePlugin(rec, tenantReq(http.MethodPost, "/api/v1/marketplace/plugins/submit", validSubmissionBody()))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("submit unavailable = %d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	srv.adminSubmissionsHandler(rec2, adminReq(http.MethodGet, "/api/v1/admin/marketplace/submissions", nil))
	if rec2.Code != http.StatusServiceUnavailable {
		t.Fatalf("admin unavailable = %d", rec2.Code)
	}
}

func TestSubmissionHandlers_SubmitCreates(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	rec := httptest.NewRecorder()
	srv.submitMarketplacePlugin(rec, tenantReq(http.MethodPost, "/api/v1/marketplace/plugins/submit", validSubmissionBody()))
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp submissionResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.State != "pending_review" {
		t.Fatalf("state=%s", resp.State)
	}
	if resp.TenantID != "tenant-a" {
		t.Fatalf("tenantID=%s", resp.TenantID)
	}
	if resp.Manifest.Slug != "stripe-payments" {
		t.Fatalf("manifest slug=%s", resp.Manifest.Slug)
	}
}

func TestSubmissionHandlers_SubmitInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/marketplace/plugins/submit", strings.NewReader("not-json"))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	srv.submitMarketplacePlugin(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d", rec.Code)
	}
}

func TestSubmissionHandlers_SubmitRejectsInvalid(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	bad := validSubmissionBody()
	bad.SubmitterEmail = "not-an-email"
	rec := httptest.NewRecorder()
	srv.submitMarketplacePlugin(rec, tenantReq(http.MethodPost, "/api/v1/marketplace/plugins/submit", bad))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
}

func TestSubmissionHandlers_AdminListGetApprove(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)

	// Create a submission first.
	createRec := httptest.NewRecorder()
	srv.submitMarketplacePlugin(createRec, tenantReq(http.MethodPost, "/api/v1/marketplace/plugins/submit", validSubmissionBody()))
	if createRec.Code != http.StatusCreated {
		t.Fatalf("create status=%d", createRec.Code)
	}
	var created submissionResponse
	_ = json.NewDecoder(createRec.Body).Decode(&created)
	id := created.ID

	// List pending.
	listRec := httptest.NewRecorder()
	srv.adminSubmissionsHandler(listRec, adminReq(http.MethodGet, "/api/v1/admin/marketplace/submissions", nil))
	if listRec.Code != http.StatusOK {
		t.Fatalf("list status=%d", listRec.Code)
	}
	var listResp submissionListResponse
	if err := json.NewDecoder(listRec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 1 {
		t.Fatalf("list total=%d want 1", listResp.Total)
	}

	// Get one.
	getRec := httptest.NewRecorder()
	srv.adminSubmissionsHandler(getRec, adminReq(http.MethodGet, "/api/v1/admin/marketplace/submissions/"+id, nil))
	if getRec.Code != http.StatusOK {
		t.Fatalf("get status=%d body=%s", getRec.Code, getRec.Body.String())
	}

	// Approve.
	approveRec := httptest.NewRecorder()
	approveBody := reviewActionRequest{ReviewNotes: "looks good"}
	srv.adminSubmissionsHandler(approveRec, adminReq(http.MethodPost, "/api/v1/admin/marketplace/submissions/"+id+"/approve", approveBody))
	if approveRec.Code != http.StatusOK {
		t.Fatalf("approve status=%d body=%s", approveRec.Code, approveRec.Body.String())
	}
	var approved submissionResponse
	_ = json.NewDecoder(approveRec.Body).Decode(&approved)
	if approved.State != "approved" {
		t.Fatalf("state after approve=%s", approved.State)
	}
	if approved.Reviewer != "admin@example.com" {
		t.Fatalf("reviewer=%s", approved.Reviewer)
	}
	if approved.ReviewNotes != "looks good" {
		t.Fatalf("review notes=%s", approved.ReviewNotes)
	}

	// Catalog should now contain the manifest.
	got, err := srv.marketplace.Catalog().GetManifest(context.Background(), "stripe-payments")
	if err != nil {
		t.Fatalf("manifest should be in catalog after approval: %v", err)
	}
	if got.Version != "1.0.0" {
		t.Fatalf("manifest version=%s", got.Version)
	}
}

func TestSubmissionHandlers_AdminReject(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	createRec := httptest.NewRecorder()
	srv.submitMarketplacePlugin(createRec, tenantReq(http.MethodPost, "/api/v1/marketplace/plugins/submit", validSubmissionBody()))
	var created submissionResponse
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	rec := httptest.NewRecorder()
	srv.adminSubmissionsHandler(rec, adminReq(http.MethodPost, "/api/v1/admin/marketplace/submissions/"+created.ID+"/reject", reviewActionRequest{ReviewNotes: "missing license"}))
	if rec.Code != http.StatusOK {
		t.Fatalf("reject status=%d body=%s", rec.Code, rec.Body.String())
	}
	var rejected submissionResponse
	_ = json.NewDecoder(rec.Body).Decode(&rejected)
	if rejected.State != "rejected" {
		t.Fatalf("state after reject=%s", rejected.State)
	}
}

func TestSubmissionHandlers_AdminInvalidTransition(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	createRec := httptest.NewRecorder()
	srv.submitMarketplacePlugin(createRec, tenantReq(http.MethodPost, "/api/v1/marketplace/plugins/submit", validSubmissionBody()))
	var created submissionResponse
	_ = json.NewDecoder(createRec.Body).Decode(&created)

	// First approve.
	approve1 := httptest.NewRecorder()
	srv.adminSubmissionsHandler(approve1, adminReq(http.MethodPost, "/api/v1/admin/marketplace/submissions/"+created.ID+"/approve", reviewActionRequest{}))
	if approve1.Code != http.StatusOK {
		t.Fatalf("first approve status=%d", approve1.Code)
	}
	// Re-approve should fail.
	approve2 := httptest.NewRecorder()
	srv.adminSubmissionsHandler(approve2, adminReq(http.MethodPost, "/api/v1/admin/marketplace/submissions/"+created.ID+"/approve", reviewActionRequest{}))
	if approve2.Code != http.StatusUnprocessableEntity {
		t.Fatalf("second approve status=%d body=%s", approve2.Code, approve2.Body.String())
	}
}

func TestSubmissionHandlers_AdminMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	rec := httptest.NewRecorder()
	srv.adminSubmissionsHandler(rec, adminReq(http.MethodDelete, "/api/v1/admin/marketplace/submissions", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec.Code)
	}
	rec2 := httptest.NewRecorder()
	srv.adminSubmissionsHandler(rec2, adminReq(http.MethodPatch, "/api/v1/admin/marketplace/submissions/sub-1/garbage", nil))
	if rec2.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status=%d", rec2.Code)
	}
}

func TestSubmissionHandlers_AdminGetNotFound(t *testing.T) {
	t.Parallel()
	srv := newSubmissionTestServer(t)
	rec := httptest.NewRecorder()
	srv.adminSubmissionsHandler(rec, adminReq(http.MethodGet, "/api/v1/admin/marketplace/submissions/missing-id", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestSubmissionRoles(t *testing.T) {
	t.Parallel()
	getReq := httptest.NewRequest(http.MethodGet, "/x", nil)
	postReq := httptest.NewRequest(http.MethodPost, "/x", nil)

	if got := submissionRole(getReq); string(got) != "viewer" {
		t.Fatalf("submission GET role=%s", got)
	}
	if got := submissionRole(postReq); string(got) != "operator" {
		t.Fatalf("submission POST role=%s", got)
	}
	if got := adminSubmissionsRole(getReq); string(got) != "admin" {
		t.Fatalf("admin role=%s", got)
	}

	if act := submissionAuditAction(getReq); act.Mutates {
		t.Fatalf("GET should not be mutating")
	}
	if act := submissionAuditAction(postReq); !act.Mutates || act.Action == "" {
		t.Fatalf("POST should be mutating with action label")
	}

	approveReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/marketplace/submissions/sub-1/approve", nil)
	if act := adminSubmissionsAuditAction(approveReq); !act.Mutates || act.Action != "marketplace.submission.approve" {
		t.Fatalf("approve audit=%v", act)
	}
	rejectReq := httptest.NewRequest(http.MethodPost, "/api/v1/admin/marketplace/submissions/sub-1/reject", nil)
	if act := adminSubmissionsAuditAction(rejectReq); act.Action != "marketplace.submission.reject" {
		t.Fatalf("reject audit=%v", act)
	}
	if act := adminSubmissionsAuditAction(getReq); act.Mutates {
		t.Fatalf("admin GET should not be mutating")
	}
}
