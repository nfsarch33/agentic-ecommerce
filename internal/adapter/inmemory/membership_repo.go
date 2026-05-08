package inmemory

import (
	"context"
	"strings"
	"sync"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// MembershipRepository is the in-memory adapter for the membership
// bounded context. Used by tests and the dev compose stack. All maps are
// keyed by (tenantID, entityID) so cross-tenant reads are impossible.
type MembershipRepository struct {
	mu sync.RWMutex

	plans        map[string]map[uuid.UUID]membership.MembershipPlan
	members      map[string]map[uuid.UUID]membership.Member
	subs         map[string]map[uuid.UUID]membership.Subscription
	subsByMember map[string]map[uuid.UUID]uuid.UUID
}

// NewMembershipRepository builds an empty in-memory MembershipRepository.
func NewMembershipRepository() *MembershipRepository {
	return &MembershipRepository{
		plans:        make(map[string]map[uuid.UUID]membership.MembershipPlan),
		members:      make(map[string]map[uuid.UUID]membership.Member),
		subs:         make(map[string]map[uuid.UUID]membership.Subscription),
		subsByMember: make(map[string]map[uuid.UUID]uuid.UUID),
	}
}

func tenantKey(tenantID string) (string, error) {
	trimmed := strings.TrimSpace(tenantID)
	if trimmed == "" {
		return "", membership.ErrTenantRequired
	}
	return trimmed, nil
}

// CreatePlan stores a fresh plan in the tenant scope. Re-creating the
// same plan id is rejected to keep parity with the postgres adapter.
func (r *MembershipRepository) CreatePlan(_ context.Context, tenantID string, plan membership.MembershipPlan) error {
	key, err := tenantKey(tenantID)
	if err != nil {
		return err
	}
	if plan.TenantID() != key {
		return membership.ErrPlanTenantMismatch
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plans[key]; !ok {
		r.plans[key] = make(map[uuid.UUID]membership.MembershipPlan)
	}
	if _, exists := r.plans[key][plan.ID()]; exists {
		return port.ErrMembershipPlanNotFound
	}
	r.plans[key][plan.ID()] = plan
	return nil
}

// UpdatePlan overwrites an existing plan within the tenant scope.
func (r *MembershipRepository) UpdatePlan(_ context.Context, tenantID string, plan membership.MembershipPlan) error {
	key, err := tenantKey(tenantID)
	if err != nil {
		return err
	}
	if plan.TenantID() != key {
		return membership.ErrPlanTenantMismatch
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	bucket, ok := r.plans[key]
	if !ok {
		return port.ErrMembershipPlanNotFound
	}
	if _, exists := bucket[plan.ID()]; !exists {
		return port.ErrMembershipPlanNotFound
	}
	bucket[plan.ID()] = plan
	return nil
}

// GetPlan fetches a plan by id within the tenant scope.
func (r *MembershipRepository) GetPlan(_ context.Context, tenantID string, planID uuid.UUID) (membership.MembershipPlan, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return membership.MembershipPlan{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	plan, ok := r.plans[key][planID]
	if !ok {
		return membership.MembershipPlan{}, port.ErrMembershipPlanNotFound
	}
	return plan, nil
}

// ListPlans returns plans for a tenant (deterministic order: by name).
func (r *MembershipRepository) ListPlans(_ context.Context, tenantID string, page, perPage int) (port.MembershipPlanList, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return port.MembershipPlanList{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	plans := make([]membership.MembershipPlan, 0, len(r.plans[key]))
	for _, plan := range r.plans[key] {
		plans = append(plans, plan)
	}
	sortPlansByName(plans)
	return paginatePlans(plans, page, perPage), nil
}

// DeletePlan removes a plan from the tenant scope.
func (r *MembershipRepository) DeletePlan(_ context.Context, tenantID string, planID uuid.UUID) error {
	key, err := tenantKey(tenantID)
	if err != nil {
		return err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.plans[key][planID]; !ok {
		return port.ErrMembershipPlanNotFound
	}
	delete(r.plans[key], planID)
	return nil
}

// CreateMember stores a fresh member in the tenant scope.
func (r *MembershipRepository) CreateMember(_ context.Context, tenantID string, member membership.Member) error {
	key, err := tenantKey(tenantID)
	if err != nil {
		return err
	}
	if member.TenantID() != key {
		return membership.ErrTenantRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.members[key]; !ok {
		r.members[key] = make(map[uuid.UUID]membership.Member)
	}
	r.members[key][member.ID()] = member
	return nil
}

// GetMember fetches a member by id within the tenant scope.
func (r *MembershipRepository) GetMember(_ context.Context, tenantID string, memberID uuid.UUID) (membership.Member, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return membership.Member{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	member, ok := r.members[key][memberID]
	if !ok {
		return membership.Member{}, port.ErrMemberNotFound
	}
	return member, nil
}

// ListMembers returns members for a tenant (deterministic order: by email).
func (r *MembershipRepository) ListMembers(_ context.Context, tenantID string, page, perPage int) (port.MembershipMemberList, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return port.MembershipMemberList{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	members := make([]membership.Member, 0, len(r.members[key]))
	for _, m := range r.members[key] {
		members = append(members, m)
	}
	sortMembersByEmail(members)
	return paginateMembers(members, page, perPage), nil
}

// CreateSubscription stores a new subscription scoped by tenant +
// member, enforcing the one-subscription-per-member invariant.
func (r *MembershipRepository) CreateSubscription(_ context.Context, tenantID string, sub membership.Subscription) error {
	key, err := tenantKey(tenantID)
	if err != nil {
		return err
	}
	if sub.TenantID() != key {
		return membership.ErrTenantRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subs[key]; !ok {
		r.subs[key] = make(map[uuid.UUID]membership.Subscription)
		r.subsByMember[key] = make(map[uuid.UUID]uuid.UUID)
	}
	if existing, ok := r.subsByMember[key][sub.MemberID()]; ok {
		if current, exists := r.subs[key][existing]; exists && !current.State().IsTerminal() {
			return port.ErrSubscriptionNotFound
		}
	}
	r.subs[key][sub.ID()] = sub
	r.subsByMember[key][sub.MemberID()] = sub.ID()
	return nil
}

// SaveSubscription overwrites a subscription within the tenant scope.
func (r *MembershipRepository) SaveSubscription(_ context.Context, tenantID string, sub membership.Subscription) error {
	key, err := tenantKey(tenantID)
	if err != nil {
		return err
	}
	if sub.TenantID() != key {
		return membership.ErrTenantRequired
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.subs[key][sub.ID()]; !ok {
		return port.ErrSubscriptionNotFound
	}
	r.subs[key][sub.ID()] = sub
	return nil
}

// GetSubscription fetches a subscription by id within the tenant scope.
func (r *MembershipRepository) GetSubscription(_ context.Context, tenantID string, subscriptionID uuid.UUID) (membership.Subscription, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return membership.Subscription{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	sub, ok := r.subs[key][subscriptionID]
	if !ok {
		return membership.Subscription{}, port.ErrSubscriptionNotFound
	}
	return sub, nil
}

// GetSubscriptionByMember fetches the latest subscription for a member.
func (r *MembershipRepository) GetSubscriptionByMember(_ context.Context, tenantID string, memberID uuid.UUID) (membership.Subscription, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return membership.Subscription{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	subID, ok := r.subsByMember[key][memberID]
	if !ok {
		return membership.Subscription{}, port.ErrSubscriptionNotFound
	}
	sub, ok := r.subs[key][subID]
	if !ok {
		return membership.Subscription{}, port.ErrSubscriptionNotFound
	}
	return sub, nil
}

// ListSubscriptions returns subscriptions for a tenant.
func (r *MembershipRepository) ListSubscriptions(_ context.Context, tenantID string, page, perPage int) (port.MembershipSubscriptionList, error) {
	key, err := tenantKey(tenantID)
	if err != nil {
		return port.MembershipSubscriptionList{}, err
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	subs := make([]membership.Subscription, 0, len(r.subs[key]))
	for _, s := range r.subs[key] {
		subs = append(subs, s)
	}
	sortSubscriptionsByCreatedAt(subs)
	return paginateSubscriptions(subs, page, perPage), nil
}
