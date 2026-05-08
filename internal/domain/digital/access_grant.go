package digital

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Source captures how an AccessGrant came to be: a paid order, an
// admin gift, or a manual admin grant. Adapters persist the string
// form; the typed constants prevent typos.
type Source string

const (
	SourcePurchase Source = "purchase"
	SourceGift     Source = "gift"
	SourceAdmin    Source = "admin"
)

// ErrInvalidSource is returned by ParseSource for unknown values.
var ErrInvalidSource = errors.New("invalid access-grant source")

// ParseSource validates and returns the canonical Source for a string.
func ParseSource(value string) (Source, error) {
	switch Source(value) {
	case SourcePurchase, SourceGift, SourceAdmin:
		return Source(value), nil
	default:
		return "", ErrInvalidSource
	}
}

// AccessGrantInput is the constructor payload.
type AccessGrantInput struct {
	TenantID   string
	CustomerID uuid.UUID
	ProductID  uuid.UUID
	LicenseID  uuid.UUID
	Source     Source
	Now        time.Time
}

// AccessGrantRecord is the repository hydration shape.
type AccessGrantRecord struct {
	ID         uuid.UUID
	TenantID   string
	CustomerID uuid.UUID
	ProductID  uuid.UUID
	LicenseID  uuid.UUID
	GrantedAt  time.Time
	Source     Source
}

// AccessGrant is the explicit join entity that records "this customer
// has access to this product via this licence". Repositories MUST
// upsert on (tenant_id, customer_id, product_id) to keep the relation
// idempotent across re-purchases.
type AccessGrant struct {
	id         uuid.UUID
	tenantID   string
	customerID uuid.UUID
	productID  uuid.UUID
	licenseID  uuid.UUID
	grantedAt  time.Time
	source     Source
}

// ErrAccessGrantSourceRequired is returned when the source is empty.
var ErrAccessGrantSourceRequired = errors.New("access grant source is required")

// NewAccessGrant constructs an AccessGrant.
func NewAccessGrant(input AccessGrantInput) (AccessGrant, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return AccessGrant{}, ErrTenantRequired
	}
	if input.CustomerID == uuid.Nil {
		return AccessGrant{}, ErrCustomerRequired
	}
	if input.ProductID == uuid.Nil {
		return AccessGrant{}, ErrProductRequired
	}
	if input.LicenseID == uuid.Nil {
		return AccessGrant{}, errors.New("access grant licence id is required")
	}
	source := input.Source
	if source == "" {
		return AccessGrant{}, ErrAccessGrantSourceRequired
	}
	if _, err := ParseSource(string(source)); err != nil {
		return AccessGrant{}, err
	}
	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	return AccessGrant{
		id:         uuid.New(),
		tenantID:   tenantID,
		customerID: input.CustomerID,
		productID:  input.ProductID,
		licenseID:  input.LicenseID,
		grantedAt:  now.UTC(),
		source:     source,
	}, nil
}

// ReconstructAccessGrant hydrates from a repository record.
func ReconstructAccessGrant(rec AccessGrantRecord) AccessGrant {
	return AccessGrant{
		id:         rec.ID,
		tenantID:   rec.TenantID,
		customerID: rec.CustomerID,
		productID:  rec.ProductID,
		licenseID:  rec.LicenseID,
		grantedAt:  rec.GrantedAt,
		source:     rec.Source,
	}
}

// Accessors.

func (g AccessGrant) ID() uuid.UUID         { return g.id }
func (g AccessGrant) TenantID() string      { return g.tenantID }
func (g AccessGrant) CustomerID() uuid.UUID { return g.customerID }
func (g AccessGrant) ProductID() uuid.UUID  { return g.productID }
func (g AccessGrant) LicenseID() uuid.UUID  { return g.licenseID }
func (g AccessGrant) GrantedAt() time.Time  { return g.grantedAt }
func (g AccessGrant) Source() Source        { return g.source }
