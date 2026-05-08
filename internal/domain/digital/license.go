package digital

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// LicenseInput is the constructor payload for a Licence.
type LicenseInput struct {
	TenantID       string
	ProductID      uuid.UUID
	CustomerID     uuid.UUID
	Key            string
	MaxActivations int
	ExpiresAt      time.Time
	Now            time.Time
}

// LicenseRecord is the repository hydration shape.
type LicenseRecord struct {
	ID             uuid.UUID
	TenantID       string
	ProductID      uuid.UUID
	CustomerID     uuid.UUID
	Key            string
	State          State
	IssuedAt       time.Time
	ExpiresAt      time.Time
	MaxActivations int
	UpdatedAt      time.Time
}

// License is the lifecycle aggregate for a customer's right to download
// a digital product. State mutations go through Apply so the state
// machine in state.go is never bypassed.
type License struct {
	id             uuid.UUID
	tenantID       string
	productID      uuid.UUID
	customerID     uuid.UUID
	key            string
	state          State
	issuedAt       time.Time
	expiresAt      time.Time
	maxActivations int
	updatedAt      time.Time
}

// ErrCustomerRequired is returned when a customer id is missing.
var ErrCustomerRequired = errors.New("licence customer id is required")

// ErrProductRequired is returned when a product id is missing.
var ErrProductRequired = errors.New("licence product id is required")

// ErrLicenseKeyRequired is returned when a licence key string is empty.
var ErrLicenseKeyRequired = errors.New("licence key is required")

// NewLicense constructs a Licence in the active state.
func NewLicense(input LicenseInput) (License, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return License{}, ErrTenantRequired
	}
	if input.ProductID == uuid.Nil {
		return License{}, ErrProductRequired
	}
	if input.CustomerID == uuid.Nil {
		return License{}, ErrCustomerRequired
	}
	key := strings.TrimSpace(input.Key)
	if key == "" {
		return License{}, ErrLicenseKeyRequired
	}
	if input.MaxActivations < 0 {
		return License{}, errors.New("licence max activations must be non-negative")
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	expiresAt := input.ExpiresAt
	if !expiresAt.IsZero() {
		expiresAt = expiresAt.UTC()
		if !expiresAt.After(now) {
			return License{}, errors.New("licence expires_at must be after issued_at")
		}
	}

	return License{
		id:             uuid.New(),
		tenantID:       tenantID,
		productID:      input.ProductID,
		customerID:     input.CustomerID,
		key:            key,
		state:          StateActive,
		issuedAt:       now,
		expiresAt:      expiresAt,
		maxActivations: input.MaxActivations,
		updatedAt:      now,
	}, nil
}

// ReconstructLicense hydrates a Licence from a repository record without
// re-validating; adapters call this after reading rows.
func ReconstructLicense(rec LicenseRecord) License {
	return License{
		id:             rec.ID,
		tenantID:       rec.TenantID,
		productID:      rec.ProductID,
		customerID:     rec.CustomerID,
		key:            rec.Key,
		state:          rec.State,
		issuedAt:       rec.IssuedAt,
		expiresAt:      rec.ExpiresAt,
		maxActivations: rec.MaxActivations,
		updatedAt:      rec.UpdatedAt,
	}
}

// Apply moves the Licence through the state machine. Caller injects
// `now` so workflow code stays deterministic.
func (l *License) Apply(t Transition, now time.Time) error {
	target, err := nextState(l.state, t)
	if err != nil {
		return err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	l.state = target
	l.updatedAt = now.UTC()
	return nil
}

// CheckActive returns nil when the licence may be used, or a typed
// error explaining why it cannot. The caller passes `now` so this
// stays deterministic in tests.
func (l License) CheckActive(now time.Time) error {
	switch l.state {
	case StateRevoked:
		return ErrLicenseRevoked
	case StateExpired:
		return ErrLicenseExpired
	}
	if !l.expiresAt.IsZero() && !now.UTC().Before(l.expiresAt) {
		return ErrLicenseExpired
	}
	return nil
}

// Accessors.

func (l License) ID() uuid.UUID         { return l.id }
func (l License) TenantID() string      { return l.tenantID }
func (l License) ProductID() uuid.UUID  { return l.productID }
func (l License) CustomerID() uuid.UUID { return l.customerID }
func (l License) Key() string           { return l.key }
func (l License) State() State          { return l.state }
func (l License) IssuedAt() time.Time   { return l.issuedAt }
func (l License) ExpiresAt() time.Time  { return l.expiresAt }
func (l License) MaxActivations() int   { return l.maxActivations }
func (l License) UpdatedAt() time.Time  { return l.updatedAt }
