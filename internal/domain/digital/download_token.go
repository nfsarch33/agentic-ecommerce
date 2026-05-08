package digital

import (
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
)

// DownloadToken is the time-limited, signed credential a customer hands
// back to /me/licenses/{id}/download to fetch a digital product file.
//
// Tokens are stored alongside licences so we can audit usage and gate
// abuse. Expired or maxed-out tokens are kept (not deleted) for
// forensic purposes.
type DownloadToken struct {
	id           uuid.UUID
	tenantID     string
	licenseID    uuid.UUID
	signature    string
	expiresAt    time.Time
	usesAllowed  int
	usesSoFar    int
	createdAt    time.Time
	lastIssuedAt time.Time
}

// DownloadTokenInput is the constructor payload.
type DownloadTokenInput struct {
	TenantID    string
	LicenseID   uuid.UUID
	Signature   string
	ExpiresAt   time.Time
	UsesAllowed int
	Now         time.Time
}

// DownloadTokenRecord is the repository hydration shape.
type DownloadTokenRecord struct {
	ID           uuid.UUID
	TenantID     string
	LicenseID    uuid.UUID
	Signature    string
	ExpiresAt    time.Time
	UsesAllowed  int
	UsesSoFar    int
	CreatedAt    time.Time
	LastIssuedAt time.Time
}

// ErrDownloadSignatureRequired is returned when a signature is empty.
var ErrDownloadSignatureRequired = errors.New("download token signature is required")

// ErrDownloadInvalidExpiry is returned when ExpiresAt is in the past
// or zero. Tokens MUST have a real expiry to limit blast radius.
var ErrDownloadInvalidExpiry = errors.New("download token expires_at must be in the future")

// ErrDownloadInvalidUsesAllowed is returned when UsesAllowed is non-
// positive.
var ErrDownloadInvalidUsesAllowed = errors.New("download token uses_allowed must be positive")

// NewDownloadToken constructs a DownloadToken with field validation.
func NewDownloadToken(input DownloadTokenInput) (DownloadToken, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return DownloadToken{}, ErrTenantRequired
	}
	if input.LicenseID == uuid.Nil {
		return DownloadToken{}, errors.New("download token licence id is required")
	}
	signature := strings.TrimSpace(input.Signature)
	if signature == "" {
		return DownloadToken{}, ErrDownloadSignatureRequired
	}

	now := input.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()

	expiresAt := input.ExpiresAt.UTC()
	if expiresAt.IsZero() || !expiresAt.After(now) {
		return DownloadToken{}, ErrDownloadInvalidExpiry
	}

	if input.UsesAllowed <= 0 {
		return DownloadToken{}, ErrDownloadInvalidUsesAllowed
	}

	return DownloadToken{
		id:           uuid.New(),
		tenantID:     tenantID,
		licenseID:    input.LicenseID,
		signature:    signature,
		expiresAt:    expiresAt,
		usesAllowed:  input.UsesAllowed,
		usesSoFar:    0,
		createdAt:    now,
		lastIssuedAt: now,
	}, nil
}

// ReconstructDownloadToken hydrates a DownloadToken from a repository
// record without re-validating.
func ReconstructDownloadToken(rec DownloadTokenRecord) DownloadToken {
	return DownloadToken{
		id:           rec.ID,
		tenantID:     rec.TenantID,
		licenseID:    rec.LicenseID,
		signature:    rec.Signature,
		expiresAt:    rec.ExpiresAt,
		usesAllowed:  rec.UsesAllowed,
		usesSoFar:    rec.UsesSoFar,
		createdAt:    rec.CreatedAt,
		lastIssuedAt: rec.LastIssuedAt,
	}
}

// CheckUsable returns nil when the token may be redeemed, or a typed
// error explaining why it cannot.
func (t DownloadToken) CheckUsable(now time.Time) error {
	if !now.UTC().Before(t.expiresAt) {
		return ErrTokenExpired
	}
	if t.usesSoFar >= t.usesAllowed {
		return ErrMaxUsesExceeded
	}
	return nil
}

// MarkUsed increments the usage counter. Callers MUST persist the
// resulting state via the repository.
func (t *DownloadToken) MarkUsed(now time.Time) error {
	if err := t.CheckUsable(now); err != nil {
		return err
	}
	t.usesSoFar++
	if now.IsZero() {
		now = time.Now().UTC()
	}
	t.lastIssuedAt = now.UTC()
	return nil
}

// Accessors.

func (t DownloadToken) ID() uuid.UUID        { return t.id }
func (t DownloadToken) TenantID() string     { return t.tenantID }
func (t DownloadToken) LicenseID() uuid.UUID { return t.licenseID }
func (t DownloadToken) Signature() string    { return t.signature }
func (t DownloadToken) ExpiresAt() time.Time { return t.expiresAt }
func (t DownloadToken) UsesAllowed() int     { return t.usesAllowed }
func (t DownloadToken) UsesSoFar() int       { return t.usesSoFar }
func (t DownloadToken) CreatedAt() time.Time { return t.createdAt }
func (t DownloadToken) LastIssuedAt() time.Time {
	return t.lastIssuedAt
}
