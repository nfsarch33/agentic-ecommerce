package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/billing"
)

func TestAdminBillingListSubscriptionsRequiresTenant(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/subscriptions", nil))
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 without tenant header, got %d", rec.Code)
	}
}

func TestAdminBillingListSubscriptionsEmpty(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/subscriptions", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d body=%s", rec.Code, rec.Body.String())
	}
	var resp adminBillingSubscriptionListResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Total != 0 {
		t.Fatalf("expected empty list, got %d", resp.Total)
	}
}

func TestAdminBillingFlow(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	sub, err := srv.billingSvc.CreateSubscription(t.Context(), billing.NewSubscriptionInput{
		ID: "sub_1", TenantID: "tenant-a", PlanID: "starter",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/subscriptions/"+sub.ID, nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("get code = %d body=%s", rec.Code, rec.Body.String())
	}

	for _, action := range []string{"pause", "resume", "cancel"} {
		switch action {
		case "pause":
			if _, err := srv.billingSvc.Activate(t.Context(), "tenant-a", sub.ID); err != nil {
				t.Fatalf("activate before pause: %v", err)
			}
		}
		req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/subscriptions/"+sub.ID+"/"+action, nil)
		req.Header.Set("X-Tenant-ID", "tenant-a")
		rec := httptest.NewRecorder()
		srv.mux().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("%s code = %d body=%s", action, rec.Code, rec.Body.String())
		}
	}
}

func TestAdminBillingListInvoicesEmpty(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/invoices", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAdminBillingUsageRollup(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/usage?plan=starter", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("usage code = %d body=%s", rec.Code, rec.Body.String())
	}
	var resp adminBillingUsageResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(resp.Rollups) != 3 {
		t.Fatalf("rollup count = %d, want 3", len(resp.Rollups))
	}
}

func TestAdminBillingMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPut, "/api/v1/admin/billing/subscriptions", bytes.NewBufferString(`{}`))
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAdminBillingSubscriptionNotFound(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/subscriptions/missing", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}
