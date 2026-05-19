package membership

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

var (
	ErrPlanNameRequired     = errors.New("membership plan name is required")
	ErrPlanCurrencyRequired = errors.New("membership plan price currency is required")
	ErrPlanPriceNotPositive = errors.New("membership plan price must be positive")
	ErrTenantRequired       = errors.New("tenant id is required")
	ErrPlanIDRequired       = errors.New("membership plan id is required")
	ErrInvalidStripePriceID = errors.New("invalid stripe price identifier")
)

// PlanInput is the constructor payload for a MembershipPlan. Use ParseFromX
// helpers (BillingCycle, money) or normalise upstream before calling.
type PlanInput struct {
	TenantID      string
	Name          string
	Description   string
	BillingCycle  BillingCycle
	Price         catalog.Money
	Benefits      []string
	StripePriceID string
}

// PlanRecord is the repository hydration shape; it bypasses the
// constructor invariants when reconstructing from persisted state.
type PlanRecord struct {
	ID            uuid.UUID
	TenantID      string
	Name          string
	Description   string
	BillingCycle  BillingCycle
	Price         catalog.Money
	Benefits      []string
	StripePriceID string
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

// MembershipPlan is the immutable description of a subscribable plan
// (cadence + price + benefits) within a tenant.
type MembershipPlan struct {
	id            uuid.UUID
	tenantID      string
	name          string
	description   string
	billingCycle  BillingCycle
	price         catalog.Money
	benefits      []string
	stripePriceID string
	createdAt     time.Time
	updatedAt     time.Time
}

// NewMembershipPlan creates a MembershipPlan after enforcing tenant scope,
// non-empty name, valid billing cycle, and a positive priced currency.
func NewMembershipPlan(input PlanInput) (MembershipPlan, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return MembershipPlan{}, ErrTenantRequired
	}
	name := strings.TrimSpace(input.Name)
	if name == "" {
		return MembershipPlan{}, ErrPlanNameRequired
	}
	if _, err := ParseBillingCycle(string(input.BillingCycle)); err != nil {
		return MembershipPlan{}, fmt.Errorf("plan %s: %w", name, err)
	}
	if input.Price.Currency() == "" {
		return MembershipPlan{}, ErrPlanCurrencyRequired
	}
	if input.Price.Amount() <= 0 {
		return MembershipPlan{}, ErrPlanPriceNotPositive
	}

	now := time.Now().UTC()
	return MembershipPlan{
		id:            uuid.New(),
		tenantID:      tenantID,
		name:          name,
		description:   strings.TrimSpace(input.Description),
		billingCycle:  input.BillingCycle,
		price:         input.Price,
		benefits:      cloneStrings(input.Benefits),
		stripePriceID: strings.TrimSpace(input.StripePriceID),
		createdAt:     now,
		updatedAt:     now,
	}, nil
}

// ReconstructPlan hydrates a MembershipPlan from a repository record.
func ReconstructPlan(rec PlanRecord) MembershipPlan {
	return MembershipPlan{
		id:            rec.ID,
		tenantID:      rec.TenantID,
		name:          rec.Name,
		description:   rec.Description,
		billingCycle:  rec.BillingCycle,
		price:         rec.Price,
		benefits:      cloneStrings(rec.Benefits),
		stripePriceID: rec.StripePriceID,
		createdAt:     rec.CreatedAt,
		updatedAt:     rec.UpdatedAt,
	}
}

// Rename produces a new MembershipPlan with the requested name change.
// Domain rules enforce non-empty trimmed name; updatedAt advances on change.
func (p MembershipPlan) Rename(name string) (MembershipPlan, error) {
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return MembershipPlan{}, ErrPlanNameRequired
	}
	if trimmed == p.name {
		return p, nil
	}
	updated := p
	updated.name = trimmed
	updated.updatedAt = time.Now().UTC()
	return updated, nil
}

// SetStripePriceID updates the Stripe linkage. Empty values clear the link.
func (p MembershipPlan) SetStripePriceID(stripePriceID string) MembershipPlan {
	updated := p
	updated.stripePriceID = strings.TrimSpace(stripePriceID)
	updated.updatedAt = time.Now().UTC()
	return updated
}

func (p MembershipPlan) ID() uuid.UUID              { return p.id }
func (p MembershipPlan) TenantID() string           { return p.tenantID }
func (p MembershipPlan) Name() string               { return p.name }
func (p MembershipPlan) Description() string        { return p.description }
func (p MembershipPlan) BillingCycle() BillingCycle { return p.billingCycle }
func (p MembershipPlan) Price() catalog.Money       { return p.price }
func (p MembershipPlan) Benefits() []string         { return cloneStrings(p.benefits) }
func (p MembershipPlan) StripePriceID() string      { return p.stripePriceID }
func (p MembershipPlan) CreatedAt() time.Time       { return p.createdAt }
func (p MembershipPlan) UpdatedAt() time.Time       { return p.updatedAt }

func cloneStrings(in []string) []string {
	if in == nil {
		return nil
	}
	out := make([]string, len(in))
	copy(out, in)
	return out
}
