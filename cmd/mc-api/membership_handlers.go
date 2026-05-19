package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
	"github.com/nfsarch33/helixon-ec/internal/domain/membership"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/nfsarch33/helixon-ec/internal/security"
)

// Re-exports of the eventbus membership event types so existing handler
// call sites stay readable. The canonical constants live in
// internal/eventbus/event.go.
const (
	MembershipCreatedEvent   = eventbus.MembershipCreated
	MembershipRenewedEvent   = eventbus.MembershipRenewed
	MembershipCancelledEvent = eventbus.MembershipCancelled
	MembershipPausedEvent    = eventbus.MembershipPaused
	MembershipResumedEvent   = eventbus.MembershipResumed
)

// membershipPlanRequest is the JSON body for POST/PATCH /membership-plans.
type membershipPlanRequest struct {
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	BillingCycle  string        `json:"billing_cycle"`
	Price         moneyResponse `json:"price"`
	Benefits      []string      `json:"benefits,omitempty"`
	StripePriceID string        `json:"stripe_price_id,omitempty"`
}

type membershipPlanResponse struct {
	ID            string        `json:"id"`
	TenantID      string        `json:"tenant_id"`
	Name          string        `json:"name"`
	Description   string        `json:"description,omitempty"`
	BillingCycle  string        `json:"billing_cycle"`
	Price         moneyResponse `json:"price"`
	Benefits      []string      `json:"benefits"`
	StripePriceID string        `json:"stripe_price_id,omitempty"`
	CreatedAt     time.Time     `json:"created_at"`
	UpdatedAt     time.Time     `json:"updated_at"`
}

type membershipPlansListResponse struct {
	Plans   []membershipPlanResponse `json:"plans"`
	Total   int                      `json:"total"`
	Page    int                      `json:"page"`
	PerPage int                      `json:"per_page"`
}

// createMembershipRequest is the JSON body for POST /memberships.
// MemberEmail is the primary identifier; the server creates the Member
// row if it does not already exist within the tenant.
type createMembershipRequest struct {
	MemberEmail string `json:"member_email"`
	PlanID      string `json:"plan_id"`
	TrialDays   int    `json:"trial_days,omitempty"`
}

type membershipResponse struct {
	ID                   string                 `json:"id"`
	TenantID             string                 `json:"tenant_id"`
	MemberID             string                 `json:"member_id"`
	MemberEmail          string                 `json:"member_email"`
	PlanID               string                 `json:"plan_id"`
	State                string                 `json:"state"`
	CurrentPeriodStart   time.Time              `json:"current_period_start"`
	CurrentPeriodEnd     time.Time              `json:"current_period_end"`
	TrialEndsAt          time.Time              `json:"trial_ends_at"`
	StripeSubscriptionID string                 `json:"stripe_subscription_id,omitempty"`
	CancelledAt          *time.Time             `json:"cancelled_at,omitempty"`
	CreatedAt            time.Time              `json:"created_at"`
	UpdatedAt            time.Time              `json:"updated_at"`
	Plan                 membershipPlanResponse `json:"plan"`
}

type membershipsListResponse struct {
	Memberships []membershipResponse `json:"memberships"`
	Total       int                  `json:"total"`
	Page        int                  `json:"page"`
	PerPage     int                  `json:"per_page"`
}

// updateMembershipRequest patches the plan or trial window.
type updateMembershipRequest struct {
	PlanID *string `json:"plan_id,omitempty"`
}

// membershipPlansHandler routes /api/v1/membership-plans*.
func (s *server) membershipPlansHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/membership-plans")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listMembershipPlans(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.createMembershipPlan(w, r)
	case path != "" && r.Method == http.MethodGet:
		s.getMembershipPlan(w, r, path)
	case path != "" && r.Method == http.MethodPatch:
		s.updateMembershipPlan(w, r, path)
	case path != "" && r.Method == http.MethodDelete:
		s.deleteMembershipPlan(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

// membershipsHandler routes /api/v1/memberships*.
func (s *server) membershipsHandler(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/memberships")
	path = strings.TrimPrefix(path, "/")

	switch {
	case path == "" && r.Method == http.MethodGet:
		s.listMemberships(w, r)
	case path == "" && r.Method == http.MethodPost:
		s.createMembership(w, r)
	case strings.HasSuffix(path, "/cancel") && r.Method == http.MethodPost:
		s.transitionMembership(w, r, strings.TrimSuffix(path, "/cancel"), membership.TransitionCancel)
	case strings.HasSuffix(path, "/pause") && r.Method == http.MethodPost:
		s.transitionMembership(w, r, strings.TrimSuffix(path, "/pause"), membership.TransitionPause)
	case strings.HasSuffix(path, "/resume") && r.Method == http.MethodPost:
		s.transitionMembership(w, r, strings.TrimSuffix(path, "/resume"), membership.TransitionResume)
	case path != "" && r.Method == http.MethodGet:
		s.getMembership(w, r, path)
	case path != "" && r.Method == http.MethodPatch:
		s.updateMembership(w, r, path)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) listMembershipPlans(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.membershipRepo.ListPlans(r.Context(), tenantID, page, perPage)
	if err != nil {
		s.log.Error("list membership plans", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	plans := make([]membershipPlanResponse, len(list.Plans))
	for i, p := range list.Plans {
		plans[i] = toMembershipPlanResponse(p)
	}
	writeJSON(w, http.StatusOK, membershipPlansListResponse{
		Plans: plans, Total: list.Total, Page: page, PerPage: perPage,
	})
}

func (s *server) getMembershipPlan(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	plan, err := s.membershipRepo.GetPlan(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrMembershipPlanNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get membership plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toMembershipPlanResponse(plan))
}

func (s *server) createMembershipPlan(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	var req membershipPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	plan, err := buildPlanFromRequest(tenantID, req)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.membershipRepo.CreatePlan(r.Context(), tenantID, plan); err != nil {
		s.log.Error("create membership plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusCreated, toMembershipPlanResponse(plan))
}

func (s *server) updateMembershipPlan(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	existing, err := s.membershipRepo.GetPlan(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrMembershipPlanNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get membership plan for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	var req membershipPlanRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	updated, err := mergePlanUpdate(existing, req)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.membershipRepo.UpdatePlan(r.Context(), tenantID, updated); err != nil {
		s.log.Error("update membership plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toMembershipPlanResponse(updated))
}

func (s *server) deleteMembershipPlan(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	if err := s.membershipRepo.DeletePlan(r.Context(), tenantID, id); err != nil {
		if errors.Is(err, port.ErrMembershipPlanNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("delete membership plan", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *server) listMemberships(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.membershipRepo.ListSubscriptions(r.Context(), tenantID, page, perPage)
	if err != nil {
		s.log.Error("list memberships", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	memberships := make([]membershipResponse, 0, len(list.Subscriptions))
	for _, sub := range list.Subscriptions {
		resp, hydrateErr := s.hydrateMembership(r.Context(), tenantID, sub)
		if hydrateErr != nil {
			continue
		}
		memberships = append(memberships, resp)
	}
	writeJSON(w, http.StatusOK, membershipsListResponse{
		Memberships: memberships, Total: list.Total, Page: page, PerPage: perPage,
	})
}

func (s *server) getMembership(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	sub, err := s.membershipRepo.GetSubscription(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrSubscriptionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get membership", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	if !s.actorMayReadMembership(r, sub) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	resp, err := s.hydrateMembership(r.Context(), tenantID, sub)
	if err != nil {
		s.log.Error("hydrate membership", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *server) createMembership(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	var req createMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	planID, err := uuid.Parse(req.PlanID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_plan_id"})
		return
	}
	plan, err := s.membershipRepo.GetPlan(r.Context(), tenantID, planID)
	if err != nil {
		if errors.Is(err, port.ErrMembershipPlanNotFound) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "plan_not_found"})
			return
		}
		s.log.Error("plan lookup for membership", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}

	member, err := s.findOrCreateMember(r.Context(), tenantID, req.MemberEmail)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}

	sub, err := membership.NewSubscription(membership.SubscriptionInput{
		TenantID:  tenantID,
		MemberID:  member.ID(),
		PlanID:    plan.ID(),
		TrialDays: req.TrialDays,
		Now:       time.Now().UTC(),
	}, plan)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": err.Error()})
		return
	}
	if err := s.membershipRepo.CreateSubscription(r.Context(), tenantID, sub); err != nil {
		s.log.Error("create subscription", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	s.publishMembershipEvent(r.Context(), MembershipCreatedEvent, tenantID, sub, member, plan)

	resp := toMembershipResponse(sub, plan, member)
	writeJSON(w, http.StatusCreated, resp)
}

func (s *server) updateMembership(w http.ResponseWriter, r *http.Request, idStr string) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	var req updateMembershipRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}
	sub, err := s.membershipRepo.GetSubscription(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrSubscriptionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get membership for update", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	if req.PlanID != nil {
		newPlanID, err := uuid.Parse(*req.PlanID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_plan_id"})
			return
		}
		newPlan, err := s.membershipRepo.GetPlan(r.Context(), tenantID, newPlanID)
		if err != nil {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "plan_not_found"})
			return
		}
		// Plan change is implemented as renew on the new plan: keep the
		// active state if active, else only mutate the planID. We rebuild
		// via Reconstruct to swap the planID atomically.
		swapped := membership.ReconstructSubscription(membership.SubscriptionRecord{
			ID: sub.ID(), TenantID: sub.TenantID(), MemberID: sub.MemberID(), PlanID: newPlan.ID(),
			State: sub.State(), CurrentPeriodStart: sub.CurrentPeriodStart(),
			CurrentPeriodEnd: sub.CurrentPeriodEnd(), TrialEndsAt: sub.TrialEndsAt(),
			StripeSubscriptionID: sub.StripeSubscriptionID(), CancelledAt: sub.CancelledAt(),
			CreatedAt: sub.CreatedAt(), UpdatedAt: time.Now().UTC(),
		})
		if err := s.membershipRepo.SaveSubscription(r.Context(), tenantID, swapped); err != nil {
			s.log.Error("save membership after plan swap", "error", err)
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
			return
		}
		sub = swapped
	}
	plan, err := s.membershipRepo.GetPlan(r.Context(), tenantID, sub.PlanID())
	if err != nil {
		s.log.Error("plan refetch", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	member, err := s.membershipRepo.GetMember(r.Context(), tenantID, sub.MemberID())
	if err != nil {
		s.log.Error("member refetch", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	writeJSON(w, http.StatusOK, toMembershipResponse(sub, plan, member))
}

func (s *server) transitionMembership(w http.ResponseWriter, r *http.Request, idStr string, transition membership.Transition) {
	tenantID, ok := s.tenantOrFail(w, r)
	if !ok {
		return
	}
	id, err := uuid.Parse(idStr)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_id"})
		return
	}
	sub, err := s.membershipRepo.GetSubscription(r.Context(), tenantID, id)
	if err != nil {
		if errors.Is(err, port.ErrSubscriptionNotFound) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "not_found"})
			return
		}
		s.log.Error("get membership for transition", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	if !s.actorMayMutateMembership(r, sub) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
		return
	}
	plan, err := s.membershipRepo.GetPlan(r.Context(), tenantID, sub.PlanID())
	if err != nil {
		s.log.Error("plan lookup for transition", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	if err := sub.Apply(transition, plan, time.Now().UTC()); err != nil {
		if errors.Is(err, membership.ErrInvalidTransition) {
			writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
			return
		}
		s.log.Error("apply transition", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	if err := s.membershipRepo.SaveSubscription(r.Context(), tenantID, sub); err != nil {
		s.log.Error("save membership after transition", "error", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
		return
	}
	member, _ := s.membershipRepo.GetMember(r.Context(), tenantID, sub.MemberID())
	s.publishMembershipEventForTransition(r.Context(), tenantID, sub, member, plan, transition)
	writeJSON(w, http.StatusOK, toMembershipResponse(sub, plan, member))
}

// findOrCreateMember resolves a Member by email within tenant scope.
// If absent, a new Member is provisioned and persisted.
func (s *server) findOrCreateMember(ctx context.Context, tenantID, email string) (membership.Member, error) {
	candidate, err := membership.NewMember(membership.MemberInput{TenantID: tenantID, Email: email})
	if err != nil {
		return membership.Member{}, err
	}
	// Look for existing member by listing (small tenants first; the
	// postgres adapter uses a unique-by-email index so duplicates can't
	// enter the table).
	list, err := s.membershipRepo.ListMembers(ctx, tenantID, 1, 100)
	if err != nil {
		return membership.Member{}, err
	}
	for _, m := range list.Members {
		if m.Email() == candidate.Email() {
			return m, nil
		}
	}
	if err := s.membershipRepo.CreateMember(ctx, tenantID, candidate); err != nil {
		return membership.Member{}, err
	}
	return candidate, nil
}

func (s *server) hydrateMembership(ctx context.Context, tenantID string, sub membership.Subscription) (membershipResponse, error) {
	plan, err := s.membershipRepo.GetPlan(ctx, tenantID, sub.PlanID())
	if err != nil {
		return membershipResponse{}, err
	}
	member, err := s.membershipRepo.GetMember(ctx, tenantID, sub.MemberID())
	if err != nil {
		return membershipResponse{}, err
	}
	return toMembershipResponse(sub, plan, member), nil
}

// tenantOrFail extracts the tenant id from the request context (set by
// withTenantRequired middleware). Returns false on missing/empty so
// handlers can short-circuit.
func (s *server) tenantOrFail(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID, _, err := s.tenantIDForScopedRequest(r)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return "", false
	}
	if string(tenantID) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return "", false
	}
	return string(tenantID), true
}

// actorMayReadMembership applies the customer-reads-own / admin-reads-any
// rule: an operator/admin can read any membership in their tenant; a
// viewer can only read memberships keyed to their own subject email.
func (s *server) actorMayReadMembership(r *http.Request, sub membership.Subscription) bool {
	actor, ok := r.Context().Value(actorContextKey{}).(requestActor)
	if !ok {
		// No actor context: middleware already gated access; default allow.
		return true
	}
	if actor.Role == security.RoleOperator || actor.Role == security.RoleAdmin {
		return true
	}
	// Viewer/customer: subject must match the membership's member email.
	member, err := s.membershipRepo.GetMember(r.Context(), sub.TenantID(), sub.MemberID())
	if err != nil {
		return false
	}
	return strings.EqualFold(actor.Subject, member.Email())
}

// actorMayMutateMembership locks down state transitions to operator/admin
// or to the customer themselves (subject == member email).
func (s *server) actorMayMutateMembership(r *http.Request, sub membership.Subscription) bool {
	return s.actorMayReadMembership(r, sub)
}

func (s *server) publishMembershipEventForTransition(ctx context.Context, tenantID string, sub membership.Subscription, member membership.Member, plan membership.MembershipPlan, transition membership.Transition) {
	switch transition {
	case membership.TransitionCancel:
		s.publishMembershipEvent(ctx, MembershipCancelledEvent, tenantID, sub, member, plan)
	case membership.TransitionPause:
		s.publishMembershipEvent(ctx, MembershipPausedEvent, tenantID, sub, member, plan)
	case membership.TransitionResume:
		s.publishMembershipEvent(ctx, MembershipResumedEvent, tenantID, sub, member, plan)
	case membership.TransitionRenew:
		s.publishMembershipEvent(ctx, MembershipRenewedEvent, tenantID, sub, member, plan)
	}
}

func (s *server) publishMembershipEvent(ctx context.Context, eventType eventbus.EventType, tenantID string, sub membership.Subscription, member membership.Member, plan membership.MembershipPlan) {
	if s.eventBus == nil {
		return
	}
	memberEmail := ""
	memberID := ""
	if member.ID() != uuid.Nil {
		memberEmail = member.Email()
		memberID = member.ID().String()
	}
	payload := eventbus.MembershipPayload{
		TenantID:       tenantID,
		SubscriptionID: sub.ID().String(),
		MemberID:       memberID,
		MemberEmail:    memberEmail,
		PlanID:         plan.ID().String(),
		PlanName:       plan.Name(),
		State:          string(sub.State()),
	}
	evt, err := eventbus.NewMembershipEvent(eventType, "mc-api.membership", time.Now().UTC(), payload)
	if err != nil {
		s.log.Warn("membership event build failed", "error", err, "type", eventType)
		return
	}
	_ = s.eventBus.Publish(ctx, evt)
}

func buildPlanFromRequest(tenantID string, req membershipPlanRequest) (membership.MembershipPlan, error) {
	cycle, err := membership.ParseBillingCycle(req.BillingCycle)
	if err != nil {
		return membership.MembershipPlan{}, err
	}
	price, err := catalog.NewMoney(req.Price.Amount, req.Price.Currency)
	if err != nil {
		return membership.MembershipPlan{}, err
	}
	return membership.NewMembershipPlan(membership.PlanInput{
		TenantID:      tenantID,
		Name:          req.Name,
		Description:   req.Description,
		BillingCycle:  cycle,
		Price:         price,
		Benefits:      req.Benefits,
		StripePriceID: req.StripePriceID,
	})
}

func mergePlanUpdate(existing membership.MembershipPlan, req membershipPlanRequest) (membership.MembershipPlan, error) {
	updated := existing
	if strings.TrimSpace(req.Name) != "" {
		renamed, err := updated.Rename(req.Name)
		if err != nil {
			return membership.MembershipPlan{}, err
		}
		updated = renamed
	}
	if req.StripePriceID != "" {
		updated = updated.SetStripePriceID(req.StripePriceID)
	}
	return updated, nil
}

func toMembershipPlanResponse(p membership.MembershipPlan) membershipPlanResponse {
	return membershipPlanResponse{
		ID:            p.ID().String(),
		TenantID:      p.TenantID(),
		Name:          p.Name(),
		Description:   p.Description(),
		BillingCycle:  string(p.BillingCycle()),
		Price:         moneyResponse{Amount: p.Price().Amount(), Currency: p.Price().Currency()},
		Benefits:      p.Benefits(),
		StripePriceID: p.StripePriceID(),
		CreatedAt:     p.CreatedAt(),
		UpdatedAt:     p.UpdatedAt(),
	}
}

func toMembershipResponse(sub membership.Subscription, plan membership.MembershipPlan, member membership.Member) membershipResponse {
	resp := membershipResponse{
		ID:                   sub.ID().String(),
		TenantID:             sub.TenantID(),
		MemberID:             sub.MemberID().String(),
		MemberEmail:          member.Email(),
		PlanID:               sub.PlanID().String(),
		State:                string(sub.State()),
		CurrentPeriodStart:   sub.CurrentPeriodStart(),
		CurrentPeriodEnd:     sub.CurrentPeriodEnd(),
		TrialEndsAt:          sub.TrialEndsAt(),
		StripeSubscriptionID: sub.StripeSubscriptionID(),
		CancelledAt:          sub.CancelledAt(),
		CreatedAt:            sub.CreatedAt(),
		UpdatedAt:            sub.UpdatedAt(),
		Plan:                 toMembershipPlanResponse(plan),
	}
	return resp
}

// membershipsRole / membershipPlansRole resolve the RBAC role required
// to access /api/v1/memberships and /api/v1/membership-plans
// respectively. Plans are admin/operator only; memberships allow viewer
// reads (the handler then enforces customer-reads-own).
func membershipsRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleViewer
	}
	return security.RoleOperator
}

func membershipPlansRole(*http.Request) security.Role {
	return security.RoleOperator
}

func membershipsAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/memberships"), "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		return auditAction{Action: "membership.create", Resource: "membership", Mutates: true}
	case path != "" && r.Method == http.MethodPatch:
		return auditAction{Action: "membership.update", Resource: path, Mutates: true}
	case strings.HasSuffix(path, "/cancel") && r.Method == http.MethodPost:
		return auditAction{Action: "membership.cancel", Resource: strings.TrimSuffix(path, "/cancel"), Mutates: true}
	case strings.HasSuffix(path, "/pause") && r.Method == http.MethodPost:
		return auditAction{Action: "membership.pause", Resource: strings.TrimSuffix(path, "/pause"), Mutates: true}
	case strings.HasSuffix(path, "/resume") && r.Method == http.MethodPost:
		return auditAction{Action: "membership.resume", Resource: strings.TrimSuffix(path, "/resume"), Mutates: true}
	default:
		return auditAction{}
	}
}

func membershipPlansAuditAction(r *http.Request) auditAction {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/membership-plans"), "/")
	switch {
	case path == "" && r.Method == http.MethodPost:
		return auditAction{Action: "membership_plan.create", Resource: "membership-plan", Mutates: true}
	case path != "" && r.Method == http.MethodPatch:
		return auditAction{Action: "membership_plan.update", Resource: path, Mutates: true}
	case path != "" && r.Method == http.MethodDelete:
		return auditAction{Action: "membership_plan.delete", Resource: path, Mutates: true}
	default:
		return auditAction{}
	}
}
