package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
)

// ErrMemberNotFound is returned when a Member cannot be located.
var ErrMemberNotFound = errors.New("member not found")

// ErrMembershipPlanNotFound is returned when a plan cannot be located.
var ErrMembershipPlanNotFound = errors.New("membership plan not found")

// ErrSubscriptionNotFound is returned when a Subscription cannot be located.
var ErrSubscriptionNotFound = errors.New("subscription not found")

// MembershipPlanList is the paginated result for plan listings.
type MembershipPlanList struct {
	Plans []membership.MembershipPlan
	Total int
}

// MembershipMemberList is the paginated result for member listings.
type MembershipMemberList struct {
	Members []membership.Member
	Total   int
}

// MembershipSubscriptionList is the paginated result for subscription
// listings.
type MembershipSubscriptionList struct {
	Subscriptions []membership.Subscription
	Total         int
}

// MembershipRepository persists members, plans, and subscriptions for the
// membership bounded context. Every method is tenant-aware so adapters
// can never accidentally cross tenant boundaries.
type MembershipRepository interface {
	CreatePlan(ctx context.Context, tenantID string, plan membership.MembershipPlan) error
	UpdatePlan(ctx context.Context, tenantID string, plan membership.MembershipPlan) error
	GetPlan(ctx context.Context, tenantID string, planID uuid.UUID) (membership.MembershipPlan, error)
	ListPlans(ctx context.Context, tenantID string, page, perPage int) (MembershipPlanList, error)
	DeletePlan(ctx context.Context, tenantID string, planID uuid.UUID) error

	CreateMember(ctx context.Context, tenantID string, member membership.Member) error
	GetMember(ctx context.Context, tenantID string, memberID uuid.UUID) (membership.Member, error)
	ListMembers(ctx context.Context, tenantID string, page, perPage int) (MembershipMemberList, error)

	CreateSubscription(ctx context.Context, tenantID string, sub membership.Subscription) error
	// SaveSubscription persists state changes only when the in-memory
	// state matches the persisted one (optimistic). Adapters must wrap
	// the update in a transaction or row lock.
	SaveSubscription(ctx context.Context, tenantID string, sub membership.Subscription) error
	GetSubscription(ctx context.Context, tenantID string, subscriptionID uuid.UUID) (membership.Subscription, error)
	GetSubscriptionByMember(ctx context.Context, tenantID string, memberID uuid.UUID) (membership.Subscription, error)
	ListSubscriptions(ctx context.Context, tenantID string, page, perPage int) (MembershipSubscriptionList, error)
}

// MembershipPaymentGateway is the abstraction over the upstream payment
// processor (Stripe today). Adapters return deterministic ids in dev
// stubs and real Stripe ids in production.
type MembershipPaymentGateway interface {
	CreateSubscription(ctx context.Context, req CreateSubscriptionRequest) (CreateSubscriptionResponse, error)
	CancelSubscription(ctx context.Context, req CancelSubscriptionRequest) error
	GetSubscription(ctx context.Context, req GetSubscriptionRequest) (PaymentSubscriptionStatus, error)
}

// CreateSubscriptionRequest is the input to CreateSubscription. The
// adapter decides how to map TenantID/MemberEmail into a Stripe
// customer; the domain stays pure.
type CreateSubscriptionRequest struct {
	TenantID       string
	SubscriptionID uuid.UUID
	MemberID       uuid.UUID
	MemberEmail    string
	StripePriceID  string
	BillingCycle   membership.BillingCycle
	TrialDays      int
}

// CreateSubscriptionResponse is the output of CreateSubscription.
type CreateSubscriptionResponse struct {
	StripeSubscriptionID string
	StripeCustomerID     string
	CurrentPeriodEnd     time.Time
}

// CancelSubscriptionRequest is the input to CancelSubscription.
type CancelSubscriptionRequest struct {
	TenantID             string
	StripeSubscriptionID string
}

// GetSubscriptionRequest is the input to GetSubscription.
type GetSubscriptionRequest struct {
	TenantID             string
	StripeSubscriptionID string
}

// PaymentSubscriptionStatus is the canonical status surface returned by
// the payment gateway adapter (Stripe).
type PaymentSubscriptionStatus struct {
	StripeSubscriptionID string
	Status               string
	CurrentPeriodEnd     time.Time
	CancelAtPeriodEnd    bool
}

// MembershipNotificationSender publishes lifecycle events to downstream
// notification channels (email, in-app, n8n webhook). Stub adapters
// simply record the events for tests; production adapters fan them out.
type MembershipNotificationSender interface {
	SendMembershipEvent(ctx context.Context, evt MembershipNotificationEvent) error
}

// MembershipNotificationEvent is the canonical payload sent through
// MembershipNotificationSender. Tenant aware by construction.
type MembershipNotificationEvent struct {
	TenantID       string
	SubscriptionID uuid.UUID
	MemberID       uuid.UUID
	MemberEmail    string
	PlanID         uuid.UUID
	PlanName       string
	State          membership.State
	Transition     membership.Transition
	OccurredAt     time.Time
}
