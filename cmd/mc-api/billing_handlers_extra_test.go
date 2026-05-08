package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/billing"
)

func TestAdminBillingGetInvoice(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	if _, err := srv.billingSvc.UpsertInvoice(context.Background(), billing.Invoice{
		ID: "inv_1", TenantID: "tenant-a", SubscriptionID: "sub_1",
		Amount: 100, Currency: "AUD", Status: billing.InvoicePaid,
	}, "invoice.paid"); err != nil {
		t.Fatalf("seed: %v", err)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/invoices/inv_1", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec.Code)
	}
}

func TestAdminBillingInvoicesMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/billing/invoices", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAdminBillingUsageMissingPlan(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/usage?plan=ghost", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d", rec.Code)
	}
}

func TestWriteBillingErrorTable(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		err  error
		want int
	}{
		{"tenant required", billing.ErrTenantRequired, http.StatusBadRequest},
		{"subscription not found", billing.ErrSubscriptionNotFound, http.StatusNotFound},
		{"invoice not found", billing.ErrInvoiceNotFound, http.StatusNotFound},
		{"plan not found", billing.ErrPlanNotFound, http.StatusNotFound},
		{"invalid transition", billing.ErrInvalidTransition, http.StatusUnprocessableEntity},
		{"already exists", billing.ErrSubscriptionAlreadyExists, http.StatusConflict},
		{"unknown", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rec := httptest.NewRecorder()
			writeBillingError(rec, tc.err)
			if rec.Code != tc.want {
				t.Fatalf("err %v -> code %d, want %d", tc.err, rec.Code, tc.want)
			}
		})
	}
}

func TestAdminBillingHandlerUnknownPath(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/billing/foo", nil)
	req.Header.Set("X-Tenant-ID", "tenant-a")
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rec.Code)
	}
}

func TestAdminBillingTransitionUnsupported(t *testing.T) {
	t.Parallel()
	srv := newTestServer(t)
	rec := httptest.NewRecorder()
	srv.adminBillingTransitionSubscription(rec, httptest.NewRequest(http.MethodPost, "/", nil), "x", billing.TransitionActivate)
	if rec.Code != http.StatusBadRequest && rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 400/405 for unsupported transition, got %d", rec.Code)
	}
}
