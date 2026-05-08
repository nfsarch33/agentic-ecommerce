package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/membership"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// MembershipRepository is the postgres adapter for the membership context.
type MembershipRepository struct {
	pool productStore
}

// NewMembershipRepository constructs a MembershipRepository over a pgx pool.
func NewMembershipRepository(pool *pgxpool.Pool) *MembershipRepository {
	return &MembershipRepository{pool: pool}
}

// CreatePlan inserts a fresh membership plan scoped by tenant.
func (r *MembershipRepository) CreatePlan(ctx context.Context, tenantID string, plan membership.MembershipPlan) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if plan.TenantID() != tenantID {
		return membership.ErrPlanTenantMismatch
	}
	const q = `
		INSERT INTO membership_plans (
			id, tenant_id, name, description, billing_cycle, price_amount, currency,
			benefits, stripe_price_id, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)`
	_, err = r.pool.Exec(ctx, q,
		plan.ID(), tenantID, plan.Name(), plan.Description(),
		string(plan.BillingCycle()), plan.Price().Amount(), plan.Price().Currency(),
		plan.Benefits(), plan.StripePriceID(), plan.CreatedAt(), plan.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert membership plan %s (tenant %s): %w", plan.ID(), tenantID, err)
	}
	return nil
}

// UpdatePlan overwrites an existing plan within the tenant scope.
func (r *MembershipRepository) UpdatePlan(ctx context.Context, tenantID string, plan membership.MembershipPlan) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if plan.TenantID() != tenantID {
		return membership.ErrPlanTenantMismatch
	}
	const q = `
		UPDATE membership_plans
		SET name = $3, description = $4, billing_cycle = $5, price_amount = $6,
		    currency = $7, benefits = $8, stripe_price_id = $9, updated_at = $10
		WHERE tenant_id = $1 AND id = $2`
	tag, err := r.pool.Exec(ctx, q,
		tenantID, plan.ID(), plan.Name(), plan.Description(),
		string(plan.BillingCycle()), plan.Price().Amount(), plan.Price().Currency(),
		plan.Benefits(), plan.StripePriceID(), plan.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("update membership plan %s (tenant %s): %w", plan.ID(), tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrMembershipPlanNotFound
	}
	return nil
}

// GetPlan fetches a plan within tenant scope.
func (r *MembershipRepository) GetPlan(ctx context.Context, tenantID string, planID uuid.UUID) (membership.MembershipPlan, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return membership.MembershipPlan{}, err
	}
	const q = `
		SELECT id, tenant_id, name, description, billing_cycle, price_amount, currency,
		       benefits, stripe_price_id, created_at, updated_at
		FROM membership_plans WHERE tenant_id = $1 AND id = $2`
	plan, err := scanPlanRow(r.pool.QueryRow(ctx, q, tenantID, planID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return membership.MembershipPlan{}, port.ErrMembershipPlanNotFound
		}
		return membership.MembershipPlan{}, err
	}
	return plan, nil
}

// ListPlans returns plans for a tenant ordered by name.
func (r *MembershipRepository) ListPlans(ctx context.Context, tenantID string, page, perPage int) (port.MembershipPlanList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.MembershipPlanList{}, err
	}
	page, perPage = normalisePagination(page, perPage)

	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, name, description, billing_cycle, price_amount, currency,
		       benefits, stripe_price_id, created_at, updated_at
		FROM membership_plans
		WHERE tenant_id = $1
		ORDER BY name ASC
		LIMIT $2 OFFSET $3`, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return port.MembershipPlanList{}, fmt.Errorf("list membership plans (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()

	var plans []membership.MembershipPlan
	for rows.Next() {
		plan, scanErr := scanPlanRow(rows)
		if scanErr != nil {
			return port.MembershipPlanList{}, scanErr
		}
		plans = append(plans, plan)
	}
	if err := rows.Err(); err != nil {
		return port.MembershipPlanList{}, err
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM membership_plans WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return port.MembershipPlanList{}, fmt.Errorf("count membership plans (tenant %s): %w", tenantID, err)
	}
	return port.MembershipPlanList{Plans: plans, Total: total}, nil
}

// DeletePlan removes a plan from the tenant scope.
func (r *MembershipRepository) DeletePlan(ctx context.Context, tenantID string, planID uuid.UUID) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	tag, err := r.pool.Exec(ctx, `DELETE FROM membership_plans WHERE tenant_id = $1 AND id = $2`, tenantID, planID)
	if err != nil {
		return fmt.Errorf("delete membership plan %s (tenant %s): %w", planID, tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrMembershipPlanNotFound
	}
	return nil
}

// CreateMember inserts a new member.
func (r *MembershipRepository) CreateMember(ctx context.Context, tenantID string, member membership.Member) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if member.TenantID() != tenantID {
		return membership.ErrTenantRequired
	}
	const q = `
		INSERT INTO memberships (id, tenant_id, email, joined_at, updated_at)
		VALUES ($1, $2, $3, $4, $5)`
	_, err = r.pool.Exec(ctx, q, member.ID(), tenantID, member.Email(), member.JoinedAt(), member.UpdatedAt())
	if err != nil {
		return fmt.Errorf("insert membership %s (tenant %s): %w", member.ID(), tenantID, err)
	}
	return nil
}

// GetMember fetches a member.
func (r *MembershipRepository) GetMember(ctx context.Context, tenantID string, memberID uuid.UUID) (membership.Member, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return membership.Member{}, err
	}
	const q = `SELECT id, tenant_id, email, joined_at, updated_at FROM memberships WHERE tenant_id = $1 AND id = $2`
	var (
		id             uuid.UUID
		tenant, email  string
		joinedAt, upAt time.Time
	)
	if err := r.pool.QueryRow(ctx, q, tenantID, memberID).Scan(&id, &tenant, &email, &joinedAt, &upAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return membership.Member{}, port.ErrMemberNotFound
		}
		return membership.Member{}, err
	}
	return membership.ReconstructMember(membership.MemberRecord{
		ID:        id,
		TenantID:  tenant,
		Email:     email,
		JoinedAt:  joinedAt,
		UpdatedAt: upAt,
	}), nil
}

// ListMembers paginates members for a tenant.
func (r *MembershipRepository) ListMembers(ctx context.Context, tenantID string, page, perPage int) (port.MembershipMemberList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.MembershipMemberList{}, err
	}
	page, perPage = normalisePagination(page, perPage)

	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, email, joined_at, updated_at
		FROM memberships
		WHERE tenant_id = $1
		ORDER BY email ASC
		LIMIT $2 OFFSET $3`, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return port.MembershipMemberList{}, fmt.Errorf("list memberships (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()

	var members []membership.Member
	for rows.Next() {
		var (
			id             uuid.UUID
			tenant, email  string
			joinedAt, upAt time.Time
		)
		if err := rows.Scan(&id, &tenant, &email, &joinedAt, &upAt); err != nil {
			return port.MembershipMemberList{}, err
		}
		members = append(members, membership.ReconstructMember(membership.MemberRecord{
			ID: id, TenantID: tenant, Email: email, JoinedAt: joinedAt, UpdatedAt: upAt,
		}))
	}
	if err := rows.Err(); err != nil {
		return port.MembershipMemberList{}, err
	}

	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM memberships WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return port.MembershipMemberList{}, err
	}
	return port.MembershipMemberList{Members: members, Total: total}, nil
}

// CreateSubscription inserts a new subscription.
func (r *MembershipRepository) CreateSubscription(ctx context.Context, tenantID string, sub membership.Subscription) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if sub.TenantID() != tenantID {
		return membership.ErrTenantRequired
	}
	const q = `
		INSERT INTO subscriptions (
			id, tenant_id, member_id, plan_id, state, current_period_start, current_period_end,
			trial_ends_at, stripe_subscription_id, cancelled_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)`
	_, err = r.pool.Exec(ctx, q,
		sub.ID(), tenantID, sub.MemberID(), sub.PlanID(), string(sub.State()),
		sub.CurrentPeriodStart(), sub.CurrentPeriodEnd(), sub.TrialEndsAt(),
		sub.StripeSubscriptionID(), sub.CancelledAt(), sub.CreatedAt(), sub.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("insert subscription %s (tenant %s): %w", sub.ID(), tenantID, err)
	}
	return nil
}

// SaveSubscription persists state changes against an existing row.
func (r *MembershipRepository) SaveSubscription(ctx context.Context, tenantID string, sub membership.Subscription) error {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return err
	}
	if sub.TenantID() != tenantID {
		return membership.ErrTenantRequired
	}
	const q = `
		UPDATE subscriptions SET
			state = $3, current_period_start = $4, current_period_end = $5,
			trial_ends_at = $6, stripe_subscription_id = $7, cancelled_at = $8,
			updated_at = $9
		WHERE tenant_id = $1 AND id = $2`
	tag, err := r.pool.Exec(ctx, q,
		tenantID, sub.ID(), string(sub.State()), sub.CurrentPeriodStart(), sub.CurrentPeriodEnd(),
		sub.TrialEndsAt(), sub.StripeSubscriptionID(), sub.CancelledAt(), sub.UpdatedAt(),
	)
	if err != nil {
		return fmt.Errorf("save subscription %s (tenant %s): %w", sub.ID(), tenantID, err)
	}
	if tag.RowsAffected() == 0 {
		return port.ErrSubscriptionNotFound
	}
	return nil
}

// GetSubscription fetches a subscription by id.
func (r *MembershipRepository) GetSubscription(ctx context.Context, tenantID string, subscriptionID uuid.UUID) (membership.Subscription, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return membership.Subscription{}, err
	}
	const q = `
		SELECT id, tenant_id, member_id, plan_id, state, current_period_start, current_period_end,
		       trial_ends_at, stripe_subscription_id, cancelled_at, created_at, updated_at
		FROM subscriptions WHERE tenant_id = $1 AND id = $2`
	sub, err := scanSubscriptionRow(r.pool.QueryRow(ctx, q, tenantID, subscriptionID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return membership.Subscription{}, port.ErrSubscriptionNotFound
		}
		return membership.Subscription{}, err
	}
	return sub, nil
}

// GetSubscriptionByMember returns the latest subscription for a member.
func (r *MembershipRepository) GetSubscriptionByMember(ctx context.Context, tenantID string, memberID uuid.UUID) (membership.Subscription, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return membership.Subscription{}, err
	}
	const q = `
		SELECT id, tenant_id, member_id, plan_id, state, current_period_start, current_period_end,
		       trial_ends_at, stripe_subscription_id, cancelled_at, created_at, updated_at
		FROM subscriptions
		WHERE tenant_id = $1 AND member_id = $2
		ORDER BY created_at DESC
		LIMIT 1`
	sub, err := scanSubscriptionRow(r.pool.QueryRow(ctx, q, tenantID, memberID))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return membership.Subscription{}, port.ErrSubscriptionNotFound
		}
		return membership.Subscription{}, err
	}
	return sub, nil
}

// ListSubscriptions returns subscriptions for a tenant.
func (r *MembershipRepository) ListSubscriptions(ctx context.Context, tenantID string, page, perPage int) (port.MembershipSubscriptionList, error) {
	tenantID, err := requireTenantID(tenantID)
	if err != nil {
		return port.MembershipSubscriptionList{}, err
	}
	page, perPage = normalisePagination(page, perPage)
	rows, err := r.pool.Query(ctx, `
		SELECT id, tenant_id, member_id, plan_id, state, current_period_start, current_period_end,
		       trial_ends_at, stripe_subscription_id, cancelled_at, created_at, updated_at
		FROM subscriptions
		WHERE tenant_id = $1
		ORDER BY created_at ASC
		LIMIT $2 OFFSET $3`, tenantID, perPage, (page-1)*perPage)
	if err != nil {
		return port.MembershipSubscriptionList{}, fmt.Errorf("list subscriptions (tenant %s): %w", tenantID, err)
	}
	defer rows.Close()

	var subs []membership.Subscription
	for rows.Next() {
		sub, scanErr := scanSubscriptionRow(rows)
		if scanErr != nil {
			return port.MembershipSubscriptionList{}, scanErr
		}
		subs = append(subs, sub)
	}
	if err := rows.Err(); err != nil {
		return port.MembershipSubscriptionList{}, err
	}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM subscriptions WHERE tenant_id = $1`, tenantID).Scan(&total); err != nil {
		return port.MembershipSubscriptionList{}, err
	}
	return port.MembershipSubscriptionList{Subscriptions: subs, Total: total}, nil
}

func normalisePagination(page, perPage int) (int, int) {
	if page < 1 {
		page = 1
	}
	if perPage < 1 || perPage > 100 {
		perPage = 20
	}
	return page, perPage
}

func scanPlanRow(row pgx.Row) (membership.MembershipPlan, error) {
	var (
		id              uuid.UUID
		tenant, name    string
		description     string
		cycle, currency string
		amount          int
		benefits        []string
		stripePriceID   string
		createdAt       time.Time
		updatedAt       time.Time
	)
	if err := row.Scan(&id, &tenant, &name, &description, &cycle, &amount, &currency, &benefits, &stripePriceID, &createdAt, &updatedAt); err != nil {
		return membership.MembershipPlan{}, err
	}
	price, err := catalog.NewMoney(amount, currency)
	if err != nil {
		return membership.MembershipPlan{}, fmt.Errorf("hydrate plan %s price: %w", id, err)
	}
	return membership.ReconstructPlan(membership.PlanRecord{
		ID: id, TenantID: tenant, Name: name, Description: description,
		BillingCycle: membership.BillingCycle(cycle), Price: price, Benefits: benefits,
		StripePriceID: stripePriceID, CreatedAt: createdAt, UpdatedAt: updatedAt,
	}), nil
}

func scanSubscriptionRow(row pgx.Row) (membership.Subscription, error) {
	var (
		id, memberID, planID                                    uuid.UUID
		tenant, state, stripeSubID                              string
		periodStart, periodEnd, trialEnds, createdAt, updatedAt time.Time
		cancelledAt                                             *time.Time
	)
	if err := row.Scan(&id, &tenant, &memberID, &planID, &state, &periodStart, &periodEnd, &trialEnds, &stripeSubID, &cancelledAt, &createdAt, &updatedAt); err != nil {
		return membership.Subscription{}, err
	}
	parsedState, err := membership.ParseState(state)
	if err != nil {
		return membership.Subscription{}, fmt.Errorf("hydrate subscription %s state %q: %w", id, state, err)
	}
	return membership.ReconstructSubscription(membership.SubscriptionRecord{
		ID: id, TenantID: tenant, MemberID: memberID, PlanID: planID, State: parsedState,
		CurrentPeriodStart: periodStart, CurrentPeriodEnd: periodEnd, TrialEndsAt: trialEnds,
		StripeSubscriptionID: stripeSubID, CancelledAt: cancelledAt,
		CreatedAt: createdAt, UpdatedAt: updatedAt,
	}), nil
}
