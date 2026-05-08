package main

import (
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/billing"
	"github.com/nfsarch33/agentic-ecommerce/internal/security"
)

// adminBillingSubscriptionResponse is the wire shape for a Subscription
// returned to admin/super-admin clients.
type adminBillingSubscriptionResponse struct {
	ID                   string    `json:"id"`
	TenantID             string    `json:"tenant_id"`
	PlanID               string    `json:"plan_id"`
	State                string    `json:"state"`
	StripeSubscriptionID string    `json:"stripe_subscription_id,omitempty"`
	StripeCustomerID     string    `json:"stripe_customer_id,omitempty"`
	CurrentPeriodStart   time.Time `json:"current_period_start"`
	CurrentPeriodEnd     time.Time `json:"current_period_end"`
	CancelAtPeriodEnd    bool      `json:"cancel_at_period_end"`
	CreatedAt            time.Time `json:"created_at"`
	UpdatedAt            time.Time `json:"updated_at"`
}

type adminBillingSubscriptionListResponse struct {
	Subscriptions []adminBillingSubscriptionResponse `json:"subscriptions"`
	Total         int                                `json:"total"`
}

type adminBillingInvoiceResponse struct {
	ID             string    `json:"id"`
	TenantID       string    `json:"tenant_id"`
	SubscriptionID string    `json:"subscription_id"`
	Amount         int       `json:"amount"`
	Currency       string    `json:"currency"`
	Status         string    `json:"status"`
	PeriodStart    time.Time `json:"period_start"`
	PeriodEnd      time.Time `json:"period_end"`
	CreatedAt      time.Time `json:"created_at"`
}

type adminBillingInvoiceListResponse struct {
	Invoices []adminBillingInvoiceResponse `json:"invoices"`
	Total    int                           `json:"total"`
}

type adminBillingUsageResponse struct {
	TenantID    string                `json:"tenant_id"`
	Plan        string                `json:"plan"`
	PeriodStart time.Time             `json:"period_start"`
	PeriodEnd   time.Time             `json:"period_end"`
	Rollups     []billing.UsageRollup `json:"rollups"`
}

// adminBillingHandler routes /api/v1/admin/billing*.
func (s *server) adminBillingHandler(w http.ResponseWriter, r *http.Request) {
	if s.billingSvc == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "billing_unconfigured"})
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/billing")
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case strings.HasPrefix(rest, "subscriptions"):
		s.dispatchAdminBillingSubscriptions(w, r, strings.TrimPrefix(rest, "subscriptions"))
	case strings.HasPrefix(rest, "invoices"):
		s.dispatchAdminBillingInvoices(w, r, strings.TrimPrefix(rest, "invoices"))
	case rest == "usage" && r.Method == http.MethodGet:
		s.adminBillingUsage(w, r)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) dispatchAdminBillingSubscriptions(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.TrimPrefix(rest, "/")
	id, action := splitTenantPath(rest)
	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.adminBillingListSubscriptions(w, r)
	case id != "" && action == "" && r.Method == http.MethodGet:
		s.adminBillingGetSubscription(w, r, id)
	case id != "" && action == "cancel" && r.Method == http.MethodPost:
		s.adminBillingTransitionSubscription(w, r, id, billing.TransitionCancel)
	case id != "" && action == "pause" && r.Method == http.MethodPost:
		s.adminBillingTransitionSubscription(w, r, id, billing.TransitionPause)
	case id != "" && action == "resume" && r.Method == http.MethodPost:
		s.adminBillingTransitionSubscription(w, r, id, billing.TransitionResume)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) dispatchAdminBillingInvoices(w http.ResponseWriter, r *http.Request, rest string) {
	rest = strings.TrimPrefix(rest, "/")
	switch {
	case rest == "" && r.Method == http.MethodGet:
		s.adminBillingListInvoices(w, r)
	case rest != "" && r.Method == http.MethodGet:
		s.adminBillingGetInvoice(w, r, rest)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
	}
}

func (s *server) adminBillingListSubscriptions(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.requireTenantHeader(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.billingSvc.ListSubscriptions(r.Context(), tenantID, page, perPage)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	out := make([]adminBillingSubscriptionResponse, len(list.Subscriptions))
	for i, sub := range list.Subscriptions {
		out[i] = toAdminBillingSubscriptionResponse(sub)
	}
	writeJSON(w, http.StatusOK, adminBillingSubscriptionListResponse{Subscriptions: out, Total: list.Total})
}

func (s *server) adminBillingGetSubscription(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := s.requireTenantHeader(w, r)
	if !ok {
		return
	}
	sub, err := s.billingSvc.GetSubscription(r.Context(), tenantID, id)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminBillingSubscriptionResponse(sub))
}

func (s *server) adminBillingTransitionSubscription(w http.ResponseWriter, r *http.Request, id string, t billing.Transition) {
	tenantID, ok := s.requireTenantHeader(w, r)
	if !ok {
		return
	}
	var (
		sub billing.Subscription
		err error
	)
	switch t {
	case billing.TransitionCancel:
		sub, err = s.billingSvc.Cancel(r.Context(), tenantID, id)
	case billing.TransitionPause:
		sub, err = s.billingSvc.Pause(r.Context(), tenantID, id)
	case billing.TransitionResume:
		sub, err = s.billingSvc.Resume(r.Context(), tenantID, id)
	default:
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method_not_allowed"})
		return
	}
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminBillingSubscriptionResponse(sub))
}

func (s *server) adminBillingListInvoices(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.requireTenantHeader(w, r)
	if !ok {
		return
	}
	page := queryInt(r, "page", 1)
	perPage := queryInt(r, "per_page", 20)
	list, err := s.billingSvc.ListInvoices(r.Context(), tenantID, page, perPage)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	out := make([]adminBillingInvoiceResponse, len(list.Invoices))
	for i, inv := range list.Invoices {
		out[i] = toAdminBillingInvoiceResponse(inv)
	}
	writeJSON(w, http.StatusOK, adminBillingInvoiceListResponse{Invoices: out, Total: list.Total})
}

func (s *server) adminBillingGetInvoice(w http.ResponseWriter, r *http.Request, id string) {
	tenantID, ok := s.requireTenantHeader(w, r)
	if !ok {
		return
	}
	inv, err := s.billingSvc.GetInvoice(r.Context(), tenantID, id)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toAdminBillingInvoiceResponse(inv))
}

func (s *server) adminBillingUsage(w http.ResponseWriter, r *http.Request) {
	tenantID, ok := s.requireTenantHeader(w, r)
	if !ok {
		return
	}
	if s.usageMeter == nil || s.billingPlans == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "usage_unconfigured"})
		return
	}
	planID := strings.TrimSpace(r.URL.Query().Get("plan"))
	if planID == "" {
		planID = "free"
	}
	plan, err := s.billingPlans.Get(r.Context(), planID)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	now := time.Now().UTC()
	periodStart := now.Add(-30 * 24 * time.Hour)
	rollups, err := billing.Snapshot(r.Context(), s.usageMeter, plan, tenantID, periodStart, now)
	if err != nil {
		writeBillingError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, adminBillingUsageResponse{
		TenantID:    tenantID,
		Plan:        plan.ID,
		PeriodStart: periodStart,
		PeriodEnd:   now,
		Rollups:     rollups,
	})
}

func (s *server) requireTenantHeader(w http.ResponseWriter, r *http.Request) (string, bool) {
	tenantID := strings.TrimSpace(r.Header.Get("X-Tenant-ID"))
	if tenantID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
		return "", false
	}
	return tenantID, true
}

func writeBillingError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, billing.ErrTenantRequired):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "tenant_required"})
	case errors.Is(err, billing.ErrSubscriptionNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "subscription_not_found"})
	case errors.Is(err, billing.ErrInvoiceNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "invoice_not_found"})
	case errors.Is(err, billing.ErrPlanNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "plan_not_found"})
	case errors.Is(err, billing.ErrInvalidTransition):
		writeJSON(w, http.StatusUnprocessableEntity, map[string]string{"error": "invalid_transition"})
	case errors.Is(err, billing.ErrSubscriptionAlreadyExists):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already_exists"})
	default:
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "internal_error"})
	}
}

func toAdminBillingSubscriptionResponse(sub billing.Subscription) adminBillingSubscriptionResponse {
	return adminBillingSubscriptionResponse{
		ID: sub.ID, TenantID: sub.TenantID, PlanID: sub.PlanID, State: string(sub.State),
		StripeSubscriptionID: sub.StripeSubscriptionID,
		StripeCustomerID:     sub.StripeCustomerID,
		CurrentPeriodStart:   sub.CurrentPeriodStart,
		CurrentPeriodEnd:     sub.CurrentPeriodEnd,
		CancelAtPeriodEnd:    sub.CancelAtPeriodEnd,
		CreatedAt:            sub.CreatedAt,
		UpdatedAt:            sub.UpdatedAt,
	}
}

func toAdminBillingInvoiceResponse(inv billing.Invoice) adminBillingInvoiceResponse {
	return adminBillingInvoiceResponse{
		ID: inv.ID, TenantID: inv.TenantID, SubscriptionID: inv.SubscriptionID,
		Amount: inv.Amount, Currency: inv.Currency, Status: string(inv.Status),
		PeriodStart: inv.PeriodStart, PeriodEnd: inv.PeriodEnd, CreatedAt: inv.CreatedAt,
	}
}

// adminBillingRole gates the /api/v1/admin/billing endpoints.
// Listing/reading is operator+; mutating actions require admin.
func adminBillingRole(r *http.Request) security.Role {
	if r.Method == http.MethodGet {
		return security.RoleOperator
	}
	return security.RoleAdmin
}

// adminBillingAuditAction tags mutating billing requests for audit.
func adminBillingAuditAction(r *http.Request) auditAction {
	if r.Method == http.MethodGet {
		return auditAction{}
	}
	rest := strings.TrimPrefix(r.URL.Path, "/api/v1/admin/billing/")
	switch {
	case strings.HasSuffix(rest, "/cancel") && r.Method == http.MethodPost:
		return auditAction{Action: "billing.subscription.cancel", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/pause") && r.Method == http.MethodPost:
		return auditAction{Action: "billing.subscription.pause", Resource: rest, Mutates: true}
	case strings.HasSuffix(rest, "/resume") && r.Method == http.MethodPost:
		return auditAction{Action: "billing.subscription.resume", Resource: rest, Mutates: true}
	default:
		return auditAction{}
	}
}
