package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
)

// TestMembershipsHandlerPauseResumeOnActiveSubscription drives the full
// pause -> resume cycle by manually advancing the subscription past
// trial via the repository, then exercising the HTTP handlers. This
// covers the renewal/pause/resume branches that the trial-only HTTP
// flow cannot reach without a workflow.
func TestMembershipsHandlerPauseResumeOnActiveSubscription(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	planRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`, "tenant-a")
	var plan membershipPlanResponse
	_ = json.NewDecoder(planRec.Body).Decode(&plan)

	memRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships",
		`{"member_email":"alice@example.com","plan_id":"`+plan.ID+`"}`, "tenant-a")
	var mem membershipResponse
	_ = json.NewDecoder(memRec.Body).Decode(&mem)

	// Force the subscription to active state directly via the repo so
	// the handler exercises the pause/resume transitions.
	subID := uuid.MustParse(mem.ID)
	planID := uuid.MustParse(plan.ID)
	memberID := uuid.MustParse(mem.MemberID)
	now := time.Now().UTC()
	active := membership.ReconstructSubscription(membership.SubscriptionRecord{
		ID: subID, TenantID: "tenant-a", MemberID: memberID, PlanID: planID,
		State: membership.StateActive, CurrentPeriodStart: now, CurrentPeriodEnd: now.Add(30 * 24 * time.Hour),
		TrialEndsAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err := srv.membershipRepo.SaveSubscription(context.Background(), "tenant-a", active); err != nil {
		t.Fatalf("force active: %v", err)
	}

	rec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+mem.ID+"/pause", "", "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("pause status = %d body=%s", rec.Code, rec.Body.String())
	}
	var paused membershipResponse
	_ = json.NewDecoder(rec.Body).Decode(&paused)
	if paused.State != "paused" {
		t.Fatalf("pause state = %s", paused.State)
	}

	rec = httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships/"+mem.ID+"/resume", "", "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("resume status = %d body=%s", rec.Code, rec.Body.String())
	}
	var resumed membershipResponse
	_ = json.NewDecoder(rec.Body).Decode(&resumed)
	if resumed.State != "active" {
		t.Fatalf("resume state = %s", resumed.State)
	}

	// Verify event bus saw membership.paused and membership.resumed.
	var paused2, resumed2 bool
	if srv.eventBus != nil {
		for _, e := range srv.eventBus.Delivered() {
			switch e.Type {
			case MembershipPausedEvent:
				paused2 = true
			case MembershipResumedEvent:
				resumed2 = true
			}
		}
	}
	if !paused2 || !resumed2 {
		t.Fatalf("missing pause/resume events: paused=%v resumed=%v", paused2, resumed2)
	}
}

// TestMembershipsHandlerListMembershipsOmitsBrokenRows exercises the
// `continue` branch in listMemberships when hydration of one row fails
// (e.g. plan deleted out from under the subscription).
func TestMembershipsHandlerListMembershipsOmitsBrokenRows(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	planRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`, "tenant-a")
	var plan membershipPlanResponse
	_ = json.NewDecoder(planRec.Body).Decode(&plan)

	memRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/memberships",
		`{"member_email":"alice@example.com","plan_id":"`+plan.ID+`"}`, "tenant-a")
	var mem membershipResponse
	_ = json.NewDecoder(memRec.Body).Decode(&mem)

	// Delete the plan; subsequent list call must skip the broken row.
	if err := srv.membershipRepo.DeletePlan(context.Background(), "tenant-a", uuid.MustParse(plan.ID)); err != nil {
		t.Fatalf("DeletePlan: %v", err)
	}

	rec := httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/memberships", "", "tenant-a")
	if rec.Code != http.StatusOK {
		t.Fatalf("list status = %d", rec.Code)
	}
	var list membershipsListResponse
	_ = json.NewDecoder(rec.Body).Decode(&list)
	if len(list.Memberships) != 0 {
		t.Fatalf("expected hydration-failed rows omitted, got %+v", list.Memberships)
	}
}

// TestMembershipsHandlerGetMembershipMemberHydrationFails covers the
// hydrateMembership error path when GetMember returns NotFound.
func TestMembershipsHandlerGetMembershipMemberHydrationFails(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	planRec := httpDoMembershipPlan(t, srv, http.MethodPost, "/api/v1/membership-plans",
		`{"name":"Gold","billing_cycle":"monthly","price":{"amount":1995,"currency":"AUD"}}`, "tenant-a")
	var plan membershipPlanResponse
	_ = json.NewDecoder(planRec.Body).Decode(&plan)

	// Insert a subscription whose memberID is bogus.
	subID := uuid.New()
	bogusMember := uuid.New()
	now := time.Now().UTC()
	sub := membership.ReconstructSubscription(membership.SubscriptionRecord{
		ID: subID, TenantID: "tenant-a", MemberID: bogusMember, PlanID: uuid.MustParse(plan.ID),
		State: membership.StateActive, CurrentPeriodStart: now, CurrentPeriodEnd: now.Add(30 * 24 * time.Hour),
		TrialEndsAt: now, CreatedAt: now, UpdatedAt: now,
	})
	if err := srv.membershipRepo.CreateSubscription(context.Background(), "tenant-a", sub); err != nil {
		t.Fatalf("seed sub: %v", err)
	}

	rec := httpDoMembershipPlan(t, srv, http.MethodGet, "/api/v1/memberships/"+subID.String(), "", "tenant-a")
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 on bad hydration", rec.Code)
	}
}

// TestTenantOrFailEdges exercises both error cases.
func TestTenantOrFailEdges(t *testing.T) {
	t.Parallel()
	srv, _ := testServer(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/membership-plans", nil)
	if got, ok := srv.tenantOrFail(rec, req); ok || got != "" {
		t.Fatalf("tenantOrFail without tenant returned %q ok=%v", got, ok)
	}
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
