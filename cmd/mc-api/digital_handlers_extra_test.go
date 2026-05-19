package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/security"
)

func TestDigitalProductsRoleResolution(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/digital-products", nil)
	if got := digitalProductsRole(r); got != security.RoleViewer {
		t.Fatalf("GET role = %q", got)
	}
	r = httptest.NewRequest(http.MethodPost, "/api/v1/digital-products", nil)
	if got := digitalProductsRole(r); got != security.RoleOperator {
		t.Fatalf("POST role = %q", got)
	}
}

func TestLicensesAuditActionTaxonomy(t *testing.T) {
	t.Parallel()
	cases := []struct {
		method, path string
		want         string
	}{
		{http.MethodPost, "/api/v1/licenses", "license.create"},
		{http.MethodPost, "/api/v1/licenses/abc/revoke", "license.revoke"},
		{http.MethodGet, "/api/v1/licenses/abc", ""},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.want, func(t *testing.T) {
			t.Parallel()
			r := httptest.NewRequest(tc.method, tc.path, nil)
			got := licensesAuditAction(r)
			if got.Action != tc.want {
				t.Fatalf("action = %q, want %q", got.Action, tc.want)
			}
		})
	}
}

func TestMeDigitalAuditActionTaxonomy(t *testing.T) {
	t.Parallel()
	r := httptest.NewRequest(http.MethodGet, "/api/v1/me/licenses", nil)
	if got := meDigitalAuditAction(r); got.Action != "me.digital.list" {
		t.Fatalf("list action = %q", got.Action)
	}
	r = httptest.NewRequest(http.MethodGet, "/api/v1/me/licenses/abc/download", nil)
	if got := meDigitalAuditAction(r); got.Action != "me.digital.download" {
		t.Fatalf("download action = %q", got.Action)
	}
}

func TestDigitalHandlersGuardOnUnconfiguredService(t *testing.T) {
	t.Parallel()
	// A bare server (no digitalSvc) should return 503 on all three
	// digital surfaces -- the production path always wires a service,
	// but we want a deterministic guard for partial deploys.
	ctx := operatorContext("tenant-a")
	bare := &server{}
	for _, fn := range []func(http.ResponseWriter, *http.Request){
		bare.digitalProductsHandler,
		bare.licensesHandler,
		bare.meDigitalLibraryHandler,
	} {
		rec := httptest.NewRecorder()
		fn(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/x", nil))
		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("unconfigured handler = %d, want 503", rec.Code)
		}
	}
}

func TestLicensesHandlerListAndGet(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")
	rec := httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/licenses", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/licenses/not-a-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid id = %d, want 400", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodGet, "/api/v1/licenses/00000000-0000-0000-0000-000000000000", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing license = %d, want 404", rec.Code)
	}
}

func TestLicensesHandlerCreateValidationErrors(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")
	rec := httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/licenses", licenseRequest{
		ProductID: "not-uuid", CustomerID: "not-uuid",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid product id = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/licenses", licenseRequest{
		ProductID: "11111111-1111-1111-1111-111111111111", CustomerID: "not-uuid",
	}))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid customer id = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/licenses", licenseRequest{
		ProductID:  "11111111-1111-1111-1111-111111111111",
		CustomerID: "22222222-2222-2222-2222-222222222222",
		Source:     "rogue",
	}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("invalid source = %d", rec.Code)
	}
	rec = httptest.NewRecorder()
	srv.licensesHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/licenses", licenseRequest{
		ProductID:  "11111111-1111-1111-1111-111111111111",
		CustomerID: "22222222-2222-2222-2222-222222222222",
	}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing product = %d", rec.Code)
	}
}

func TestDigitalProductsHandlerCreateValidationErrors(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")
	rec := httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodPost, "/api/v1/digital-products", digitalProductRequest{
		// Missing required fields.
	}))
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("missing fields = %d", rec.Code)
	}
}

func TestDigitalProductsHandlerInvalidJSON(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")
	req := httptest.NewRequest(http.MethodPost, "/api/v1/digital-products", nil).WithContext(ctx)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	srv.digitalProductsHandler(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("invalid json = %d", rec.Code)
	}
}

func TestCustomerOrFailRejectsUnauthenticated(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	// Include the tenant header so tenantOrFail does not short-
	// circuit before customerOrFail runs.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/me/licenses", nil).WithContext(context.Background())
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.meDigitalLibraryHandler(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauth = %d, want 401", rec.Code)
	}
}

func TestAdminDigitalProductDownloadRequiresGrant(t *testing.T) {
	t.Parallel()
	srv, _ := newDigitalTestServer(t)
	ctx := operatorContext("tenant-a")
	// Missing grant: the helper short-circuits with 404 not_granted.
	rec := httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodGet,
		"/api/v1/digital-products/11111111-1111-1111-1111-111111111111/download?customer_id=22222222-2222-2222-2222-222222222222", nil))
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing grant = %d, want 404", rec.Code)
	}
	// Bad customer_id.
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodGet,
		"/api/v1/digital-products/11111111-1111-1111-1111-111111111111/download?customer_id=not-uuid", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad customer = %d", rec.Code)
	}
	// Bad product id.
	rec = httptest.NewRecorder()
	srv.digitalProductsHandler(rec, newJSONRequest(t, ctx, http.MethodGet,
		"/api/v1/digital-products/not-uuid/download?customer_id=22222222-2222-2222-2222-222222222222", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("bad product = %d", rec.Code)
	}
}

// json import keeps the file go-vet clean even if some sub-tests
// remove their JSON usage during refactors.
var _ = json.NewDecoder
