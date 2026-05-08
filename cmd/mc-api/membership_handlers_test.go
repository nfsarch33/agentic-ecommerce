package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const tenantHeader = "X-Tenant-ID"

func httpDoMembershipPlan(t *testing.T, srv *server, method, path, body string, tenantID string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if tenantID != "" {
		req.Header.Set(tenantHeader, tenantID)
	}
	rec := httptest.NewRecorder()
	srv.mux().ServeHTTP(rec, req)
	return rec
}

func TestMembershipPlansHandlerCRUD(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	planBody := `{"name":"Gold","description":"Best plan","billing_cycle":"monthly","price":{"amount":4995,"currency":"AUD"},"benefits":["priority_support"],"stripe_price_id":"price_dev_1"}`

	rec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans", planBody, "tenant-a")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created membershipPlanResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.Name != "Gold" || created.Price.Amount != 4995 {
		t.Fatalf("created = %+v", created)
	}

	rec = httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/membership-plans", "", "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list membershipPlansListResponse
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if list.Total != 1 {
		t.Fatalf("list total = %d", list.Total)
	}

	rec = httpDoMembershipPlan(t, srv, http.MethodPatch, "/api/v1/membership-plans/"+created.ID, `{"name":"Platinum","billing_cycle":"monthly","price":{"amount":4995,"currency":"AUD"}}`, "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("patch status = %d body=%s", rec.Code, rec.Body.String())
	}
	var updated membershipPlanResponse
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode patch: %v", err)
	}
	if updated.Name != "Platinum" {
		t.Fatalf("patch name = %s, want Platinum", updated.Name)
	}

	rec = httpDoMembershipPlan(t, srv, http.MethodDelete, "/api/v1/membership-plans/"+created.ID, "", "tenant-a")
	if rec.Code != http.StatusNoContent {
		t.Fatalf("delete status = %d", rec.Code)
	}
}

func TestMembershipPlansHandlerRequiresTenant(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	rec := httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/membership-plans", "", "")
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 for missing tenant", rec.Code)
	}
}

func TestMembershipPlansHandlerRejectsBadInput(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	cases := []struct {
		name   string
		method string
		body   string
		want   int
	}{
		{name: "invalid json", method: http.MethodPost, body: "{bad", want: http.StatusBadRequest},
		{name: "invalid cycle", method: http.MethodPost, body: `{"name":"Bad","billing_cycle":"daily","price":{"amount":1,"currency":"AUD"}}`, want: http.StatusUnprocessableEntity},
		{name: "missing name", method: http.MethodPost, body: `{"billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`, want: http.StatusUnprocessableEntity},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			rec := httpDoMembershipPlan(t, srv, tc.method, "/api/v1/membership-plans", tc.body, "tenant-a")
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d body=%s", rec.Code, tc.want, rec.Body.String())
			}
		})
	}
}

func TestMembershipsHandlerCreateGetCancel(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	planRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"},"stripe_price_id":"price_dev_x"}`,
		"tenant-a")
	var plan membershipPlanResponse
	if err := json.NewDecoder(planRec.Body).Decode(&plan); err != nil {
		t.Fatalf("plan decode: %v", err)
	}

	body := `{"member_email":"alice@example.com","plan_id":"` + plan.ID + `","trial_days":7}`
	rec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships", body, "tenant-a")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d body=%s", rec.Code, rec.Body.String())
	}
	var created membershipResponse
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if created.MemberEmail != "alice@example.com" || created.State != "trial" {
		t.Fatalf("created = %+v", created)
	}

	rec = httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/memberships/"+created.ID, "", "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("get status = %d body=%s", rec.Code, rec.Body.String())
	}

	rec = httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+created.ID+"/cancel", "", "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status = %d body=%s", rec.Code, rec.Body.String())
	}
	var cancelled membershipResponse
	if err := json.NewDecoder(rec.Body).Decode(&cancelled); err != nil {
		t.Fatalf("decode cancel: %v", err)
	}
	if cancelled.State != "cancelled" || cancelled.CancelledAt == nil {
		t.Fatalf("cancelled = %+v", cancelled)
	}

	// Pause-after-cancel must fail (terminal state).
	rec = httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+created.ID+"/pause", "", "tenant-a")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("pause-after-cancel status = %d, want 422", rec.Code)
	}
}

func TestMembershipsHandlerPauseResumeRoundtrip(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	planRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`,
		"tenant-a")
	var plan membershipPlanResponse
	_ = json.NewDecoder(planRec.Body).Decode(&plan)

	rec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships",
		`{"member_email":"bob@example.com","plan_id":"`+plan.ID+`"}`, "tenant-a")
	var created membershipResponse
	_ = json.NewDecoder(rec.Body).Decode(&created)

	// Cannot pause from trial.
	rec = httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+created.ID+"/pause", "", "tenant-a")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("pause from trial = %d, want 422", rec.Code)
	}

	// Resume from trial also rejected.
	rec = httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+created.ID+"/resume", "", "tenant-a")
	if rec.Code != http.StatusUnprocessableEntity {
		t.Fatalf("resume from trial = %d, want 422", rec.Code)
	}
}

func TestMembershipsHandlerTenantIsolation(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	planA := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`,
		"tenant-a")
	var pa membershipPlanResponse
	_ = json.NewDecoder(planA.Body).Decode(&pa)

	memA := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships",
		`{"member_email":"alice@example.com","plan_id":"`+pa.ID+`"}`, "tenant-a")
	var ma membershipResponse
	_ = json.NewDecoder(memA.Body).Decode(&ma)

	// Cross-tenant fetch must 404.
	rec := httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/memberships/"+ma.ID, "", "tenant-b")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant get = %d body=%s, want 404", rec.Code, rec.Body.String())
	}

	// Cross-tenant cancel must 404.
	rec = httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+ma.ID+"/cancel", "", "tenant-b")
	if rec.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant cancel = %d, want 404", rec.Code)
	}

	// Tenant b list must be empty.
	rec = httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/memberships", "", "tenant-b")
	if rec.Code != http.StatusOK {
		t.Fatalf("list tenant-b = %d", rec.Code)
	}
	var list membershipsListResponse
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if list.Total != 0 || len(list.Memberships) != 0 {
		t.Fatalf("tenant-b sees tenant-a data: %+v", list)
	}
}

func TestMembershipsHandlerMethodNotAllowed(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	rec := httpDoMembershipPlan(t, srv, http.MethodDelete, "/api/v1/memberships", "", "tenant-a")
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status = %d, want 405", rec.Code)
	}
}

func TestMembershipsHandlerEmitsEvents(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)

	planRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`,
		"tenant-a")
	var plan membershipPlanResponse
	_ = json.NewDecoder(planRec.Body).Decode(&plan)

	rec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships",
		`{"member_email":"alice@example.com","plan_id":"`+plan.ID+`"}`, "tenant-a")
	var created membershipResponse
	_ = json.NewDecoder(rec.Body).Decode(&created)

	httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+created.ID+"/cancel", "", "tenant-a")

	// Verify event bus received membership.created and membership.cancelled.
	if srv.eventBus == nil {
		t.Skip("event bus not wired in test server")
	}
	delivered := srv.eventBus.Delivered()
	var sawCreated, sawCancelled bool
	for _, e := range delivered {
		switch e.Type {
		case MembershipCreatedEvent:
			sawCreated = true
		case MembershipCancelledEvent:
			sawCancelled = true
		}
		if e.TenantID != "tenant-a" {
			t.Fatalf("event tenant = %s, want tenant-a", e.TenantID)
		}
	}
	if !sawCreated {
		t.Error("missing membership.created event")
	}
	if !sawCancelled {
		t.Error("missing membership.cancelled event")
	}
}

func TestMembershipPlanResponseBodyStable(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	rec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Silver","billing_cycle":"annual","price":{"amount":29900,"currency":"AUD"}}`, "tenant-a")
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status = %d", rec.Code)
	}
	body := rec.Body.String()
	for _, expected := range []string{"\"id\"", "\"tenant_id\":\"tenant-a\"", "\"billing_cycle\":\"annual\""} {
		if !strings.Contains(body, expected) {
			t.Fatalf("body missing %s; full body=%s", expected, body)
		}
	}
}
