package membership

import (
	"errors"
	"net/mail"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	ErrMemberEmailRequired = errors.New("membership member email is required")
	ErrMemberEmailInvalid  = errors.New("membership member email is invalid")
)

// MemberInput is the constructor payload for a Member.
type MemberInput struct {
	TenantID string
	Email    string
}

// MemberRecord is the repository hydration shape.
type MemberRecord struct {
	ID        uuid.UUID
	TenantID  string
	Email     string
	JoinedAt  time.Time
	UpdatedAt time.Time
}

// Member is the customer-facing identity within the membership context.
// One Member can have at most one active Subscription per tenant.
type Member struct {
	id        uuid.UUID
	tenantID  string
	email     string
	joinedAt  time.Time
	updatedAt time.Time
}

// NewMember creates a Member after validating tenant scope and email.
func NewMember(input MemberInput) (Member, error) {
	tenantID := strings.TrimSpace(input.TenantID)
	if tenantID == "" {
		return Member{}, ErrTenantRequired
	}
	email, err := normaliseMemberEmail(input.Email)
	if err != nil {
		return Member{}, err
	}
	now := time.Now().UTC()
	return Member{
		id:        uuid.New(),
		tenantID:  tenantID,
		email:     email,
		joinedAt:  now,
		updatedAt: now,
	}, nil
}

// ReconstructMember hydrates a Member from a repository record.
func ReconstructMember(rec MemberRecord) Member {
	return Member{
		id:        rec.ID,
		tenantID:  rec.TenantID,
		email:     rec.Email,
		joinedAt:  rec.JoinedAt,
		updatedAt: rec.UpdatedAt,
	}
}

func (m Member) ID() uuid.UUID        { return m.id }
func (m Member) TenantID() string     { return m.tenantID }
func (m Member) Email() string        { return m.email }
func (m Member) JoinedAt() time.Time  { return m.joinedAt }
func (m Member) UpdatedAt() time.Time { return m.updatedAt }

func normaliseMemberEmail(value string) (string, error) {
	email := strings.ToLower(strings.TrimSpace(value))
	if email == "" {
		return "", ErrMemberEmailRequired
	}
	parsed, err := mail.ParseAddress(email)
	if err != nil || parsed.Address != email || !strings.Contains(email, "@") {
		return "", ErrMemberEmailInvalid
	}
	return email, nil
}
