package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/tenant"
)

func newTenantAdminTestServer(t *testing.T) *server {
	t.Helper()
	repo := tenant.NewInMemoryAggregateRepository()
	svc := tenant.NewAggregateService(repo)
	return &server{tenantAggregateSvc: svc}
}

func doTenantAdminRequest(t *testing.T, srv *server, method, path string, body []byte) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(method, path, bytes.NewReader(body))
	srv.tenantAdminHandler(rec, req)
	return rec
}

func TestTenantAdminCreate(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	body, _ := json.Marshal(tenantAdminCreateRequest{Slug: "acme", Name: "Acme"})
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", body)
	if rec.Code != http.StatusCreated {
		t.Fatalf("status = %d, body=%s", rec.Code, rec.Body.String())
	}
	var resp tenantAdminResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Status != "provisioning" {
		t.Fatalf("status = %s, want provisioning", resp.Status)
	}
}

func TestTenantAdminCreateRejectsBadSlug(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	body, _ := json.Marshal(tenantAdminCreateRequest{Slug: "ACME", Name: "Acme"})
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", body)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400, body=%s", rec.Code, rec.Body.String())
	}
}

func TestTenantAdminCreateDuplicate(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	body, _ := json.Marshal(tenantAdminCreateRequest{Slug: "acme", Name: "Acme"})
	if rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", body); rec.Code != http.StatusCreated {
		t.Fatalf("first create: %d", rec.Code)
	}
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", body)
	if rec.Code != http.StatusConflict {
		t.Fatalf("expected 409 conflict, got %d", rec.Code)
	}
}

func TestTenantAdminListAndGet(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	if _, err := srv.tenantAggregateSvc.Create(context.Background(), tenant.CreateTenantInput{Slug: "acme", Name: "Acme"}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	rec := doTenantAdminRequest(t, srv, http.MethodGet, "/api/v1/tenants", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var listResp tenantAdminListResponse
	if err := json.NewDecoder(rec.Body).Decode(&listResp); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if listResp.Total != 1 {
		t.Fatalf("total = %d, want 1", listResp.Total)
	}
	rec = doTenantAdminRequest(t, srv, http.MethodGet, "/api/v1/tenants/acme", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d", rec.Code)
	}
}

func TestTenantAdminLifecycle(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	tnt, err := srv.tenantAggregateSvc.Create(context.Background(), tenant.CreateTenantInput{Slug: "acme", Name: "Acme"})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	id := string(tnt.ID)
	// Activate
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/"+id+"/activate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("activate: %d", rec.Code)
	}
	// Suspend
	rec = doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/"+id+"/suspend", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("suspend: %d", rec.Code)
	}
	// Activate again
	rec = doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/"+id+"/activate", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("re-activate: %d", rec.Code)
	}
	// Archive
	rec = doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/"+id+"/archive", nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("archive: %d", rec.Code)
	}
	// Activate from archived must fail
	rec = doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/"+id+"/activate", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 for activate-from-archived, got %d", rec.Code)
	}
}

func TestTenantAdminUpdate(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	tnt, _ := srv.tenantAggregateSvc.Create(context.Background(), tenant.CreateTenantInput{Slug: "acme", Name: "Acme"})
	name, plan := "Renamed", "pro"
	body, _ := json.Marshal(tenantAdminUpdateRequest{Name: &name, Plan: &plan})
	rec := doTenantAdminRequest(t, srv, http.MethodPatch, "/api/v1/tenants/"+string(tnt.ID), body)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	var resp tenantAdminResponse
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp.Name != "Renamed" {
		t.Fatalf("name = %s", resp.Name)
	}
	if resp.Plan != "pro" {
		t.Fatalf("plan = %s", resp.Plan)
	}
}

func TestTenantAdminUnconfigured(t *testing.T) {
	t.Parallel()
	srv := &server{}
	rec := doTenantAdminRequest(t, srv, http.MethodGet, "/api/v1/tenants", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", rec.Code)
	}
}

func TestTenantAdminGetNotFound(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodGet, "/api/v1/tenants/ghost", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", rec.Code)
	}
}

func TestTenantAdminInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", []byte("not json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}

func TestTenantAdminMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodDelete, "/api/v1/tenants", nil)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestTenantAdminRoleAndAudit(t *testing.T) {
	t.Parallel()
	if got := tenantAdminRole(httptest.NewRequest(http.MethodGet, "/api/v1/tenants", nil)); got != "operator" {
		t.Fatalf("GET role = %s, want operator", got)
	}
	if got := tenantAdminRole(httptest.NewRequest(http.MethodPost, "/api/v1/tenants", nil)); got != "admin" {
		t.Fatalf("POST role = %s, want admin", got)
	}
	suspend := httptest.NewRequest(http.MethodPost, "/api/v1/tenants/acme/suspend", nil)
	if action := tenantAdminAuditAction(suspend); !action.Mutates || !strings.Contains(action.Action, "suspend") {
		t.Fatalf("suspend audit = %+v", action)
	}
	create := httptest.NewRequest(http.MethodPost, "/api/v1/tenants", nil)
	if action := tenantAdminAuditAction(create); action.Action != "tenant.create" {
		t.Fatalf("create audit = %+v", action)
	}
}

func TestTenantAdminAuditActionAllPaths(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method  string
		path    string
		want    string
		mutates bool
	}{
		{http.MethodGet, "/api/v1/tenants", "", false},
		{http.MethodGet, "/api/v1/tenants/acme", "", false},
		{http.MethodPost, "/api/v1/tenants", "tenant.create", true},
		{http.MethodPost, "/api/v1/tenants/acme/suspend", "tenant.suspend", true},
		{http.MethodPost, "/api/v1/tenants/acme/activate", "tenant.activate", true},
		{http.MethodPost, "/api/v1/tenants/acme/archive", "tenant.archive", true},
		{http.MethodPatch, "/api/v1/tenants/acme", "tenant.update", true},
	}
	for _, tc := range cases {
		t.Run(tc.method+tc.path, func(t *testing.T) {
			t.Parallel()
			req := httptest.NewRequest(tc.method, tc.path, nil)
			got := tenantAdminAuditAction(req)
			if got.Mutates != tc.mutates {
				t.Fatalf("mutates = %v, want %v", got.Mutates, tc.mutates)
			}
			if got.Action != tc.want {
				t.Fatalf("action = %q, want %q", got.Action, tc.want)
			}
		})
	}
}

func TestTenantAdminGetMissing(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodGet, "/api/v1/tenants/missing-id", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown id, got %d", rec.Code)
	}
}

func TestTenantAdminUpdateNotFound(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	name := "Renamed"
	body, _ := json.Marshal(tenantAdminUpdateRequest{Name: &name})
	rec := doTenantAdminRequest(t, srv, http.MethodPatch, "/api/v1/tenants/ghost", body)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTenantAdminSuspendInvalidTransition(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	tnt, _ := srv.tenantAggregateSvc.Create(context.Background(), tenant.CreateTenantInput{Slug: "acme", Name: "Acme"})
	// Suspend from provisioning is not allowed -> 422
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/"+string(tnt.ID)+"/suspend", nil)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422, got %d", rec.Code)
	}
}

func TestTenantAdminUpdateInvalidJSON(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	tnt, _ := srv.tenantAggregateSvc.Create(context.Background(), tenant.CreateTenantInput{Slug: "acme", Name: "Acme"})
	rec := doTenantAdminRequest(t, srv, http.MethodPatch, "/api/v1/tenants/"+string(tnt.ID), []byte("not json"))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestTenantAdminSuspendNotFound(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/ghost/suspend", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTenantAdminArchiveNotFound(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/ghost/archive", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTenantAdminActivateNotFound(t *testing.T) {
	t.Parallel()
	srv := newTenantAdminTestServer(t)
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants/ghost/activate", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestTenantAdminCreateInvalidQuotaError(t *testing.T) {
	t.Parallel()
	repo := tenant.NewInMemoryAggregateRepository()
	svc := tenant.NewAggregateService(repo).WithQuota(1)
	srv := &server{tenantAggregateSvc: svc}
	body, _ := json.Marshal(tenantAdminCreateRequest{Slug: "acme", Name: "Acme"})
	if rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", body); rec.Code != http.StatusCreated {
		t.Fatalf("first: %d", rec.Code)
	}
	body2, _ := json.Marshal(tenantAdminCreateRequest{Slug: "two", Name: "Two"})
	rec := doTenantAdminRequest(t, srv, http.MethodPost, "/api/v1/tenants", body2)
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("expected 422 quota, got %d", rec.Code)
	}
}
