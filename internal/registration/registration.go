// Package registration implements the v2.5.0 tenant self-service
// registration workflow. The flow is:
//
//	pending_email_verification -> email_verified -> onboarding -> active
//
// On success we hand off to internal/tenant.AggregateService.Create
// to provision the actual Tenant aggregate. Verification tokens are
// HMAC-signed (mirroring internal/adapter/signedurl/issuer.go and the
// v2.3.0 license-key generator) so we can verify them stateless from
// any node in the mc-api fleet.
package registration

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Status is the lifecycle status of a RegistrationRequest.
type Status string

const (
	// StatusPendingEmailVerification is the initial status set by Submit.
	StatusPendingEmailVerification Status = "pending_email_verification"
	// StatusEmailVerified is reached when the user clicks the link in
	// the verification email and the token validates.
	StatusEmailVerified Status = "email_verified"
	// StatusOnboarding is reached when the user starts the onboarding
	// wizard (POST /register/onboarding).
	StatusOnboarding Status = "onboarding"
	// StatusActive is the terminal status; a Tenant aggregate has
	// been provisioned and the registration record is preserved for
	// audit.
	StatusActive Status = "active"
)

// String returns the canonical string for a Status.
func (s Status) String() string { return string(s) }

// IsTerminal reports whether the Status permits any further transitions.
func (s Status) IsTerminal() bool { return s == StatusActive }

// statusTransitionTable encodes the legal status moves. Anything
// missing is illegal and produces ErrInvalidTransition.
var statusTransitionTable = map[Status]map[Status]struct{}{
	StatusPendingEmailVerification: {StatusEmailVerified: {}},
	StatusEmailVerified:            {StatusOnboarding: {}},
	StatusOnboarding:               {StatusActive: {}},
}

// canTransition returns nil when from -> to is permitted.
func canTransition(from, to Status) error {
	moves, ok := statusTransitionTable[from]
	if !ok {
		return fmt.Errorf("%w: %s is terminal", ErrInvalidTransition, from)
	}
	if _, ok := moves[to]; !ok {
		return fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, to)
	}
	return nil
}

// Errors returned by the registration package.
var (
	// ErrSecretTooShort is returned when NewIssuer is given a secret
	// shorter than MinHMACSecretBytes.
	ErrSecretTooShort = errors.New("registration hmac secret too short")
	// ErrTokenInvalid is returned when token verification fails:
	// missing fields, signature mismatch, expired, etc.
	ErrTokenInvalid = errors.New("registration token invalid")
	// ErrTokenExpired is returned by VerifyToken when the token's
	// expiry timestamp is in the past.
	ErrTokenExpired = errors.New("registration token expired")
	// ErrEmailRequired is returned when the email is empty or fails
	// the syntactic check.
	ErrEmailRequired = errors.New("registration email required")
	// ErrSlugRequired is returned when the slug is empty or fails the
	// kebab-case check.
	ErrSlugRequired = errors.New("registration slug required")
	// ErrSlugTaken is returned when the slug is already in use by an
	// existing Tenant.
	ErrSlugTaken = errors.New("registration slug taken")
	// ErrAlreadyVerified is returned when the user attempts to verify
	// a registration that is already past pending_email_verification.
	ErrAlreadyVerified = errors.New("registration already verified")
	// ErrAlreadyActive is returned when the user attempts to onboard
	// an already-active registration.
	ErrAlreadyActive = errors.New("registration already active")
	// ErrInvalidTransition is returned when an illegal Status move is
	// requested.
	ErrInvalidTransition = errors.New("registration invalid status transition")
)

// MinHMACSecretBytes is the minimum length we accept for the
// registration HMAC secret. Same lower bound as the Stripe webhook
// and the digital signed-url issuer.
const MinHMACSecretBytes = 32

// DefaultTokenTTL is the default lifetime of a verification token.
const DefaultTokenTTL = 24 * time.Hour

// Request is the persisted registration aggregate. The token is not
// stored as plaintext; we re-derive the canonical signature on
// verification and compare in constant time.
type Request struct {
	ID            string    `json:"id"`
	Email         string    `json:"email"`
	SlugRequested string    `json:"slug_requested"`
	PlanRequested string    `json:"plan_requested"`
	Status        Status    `json:"status"`
	TenantID      string    `json:"tenant_id,omitempty"`
	CompanyName   string    `json:"company_name,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	VerifiedAt    time.Time `json:"verified_at,omitempty"`
	OnboardedAt   time.Time `json:"onboarded_at,omitempty"`
	ActivatedAt   time.Time `json:"activated_at,omitempty"`
	ExpiresAt     time.Time `json:"expires_at"`
}

// SubmitInput is the input to NewRequest. Email + slug are required;
// planRequested defaults to "free".
type SubmitInput struct {
	Email         string
	SlugRequested string
	PlanRequested string
	Now           time.Time
}

// NewRequest validates inputs and returns a fresh Request in
// StatusPendingEmailVerification.
func NewRequest(in SubmitInput, ttl time.Duration) (Request, error) {
	email := strings.ToLower(strings.TrimSpace(in.Email))
	if !looksLikeEmail(email) {
		return Request{}, fmt.Errorf("%w: %q", ErrEmailRequired, in.Email)
	}
	slug := strings.TrimSpace(in.SlugRequested)
	if slug == "" || !slugPattern(slug) {
		return Request{}, fmt.Errorf("%w: %q", ErrSlugRequired, in.SlugRequested)
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now().UTC()
	}
	now = now.UTC()
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	plan := strings.TrimSpace(in.PlanRequested)
	if plan == "" {
		plan = "free"
	}
	id, err := newID()
	if err != nil {
		return Request{}, err
	}
	return Request{
		ID:            id,
		Email:         email,
		SlugRequested: slug,
		PlanRequested: plan,
		Status:        StatusPendingEmailVerification,
		CreatedAt:     now,
		ExpiresAt:     now.Add(ttl),
	}, nil
}

// MarkVerified moves the request from pending_email_verification to
// email_verified. Errors when the prior state is not pending.
func (r Request) MarkVerified(now time.Time) (Request, error) {
	if r.Status == StatusEmailVerified || r.Status == StatusOnboarding || r.Status == StatusActive {
		return r, ErrAlreadyVerified
	}
	if err := canTransition(r.Status, StatusEmailVerified); err != nil {
		return Request{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r.Status = StatusEmailVerified
	r.VerifiedAt = now.UTC()
	return r, nil
}

// MarkOnboarding records the start of the onboarding wizard.
func (r Request) MarkOnboarding(companyName string, now time.Time) (Request, error) {
	if r.Status == StatusActive {
		return r, ErrAlreadyActive
	}
	if err := canTransition(r.Status, StatusOnboarding); err != nil {
		return Request{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r.Status = StatusOnboarding
	r.CompanyName = strings.TrimSpace(companyName)
	r.OnboardedAt = now.UTC()
	return r, nil
}

// MarkActive records successful onboarding and stores the tenant id.
func (r Request) MarkActive(tenantID string, now time.Time) (Request, error) {
	if r.Status == StatusActive {
		return r, ErrAlreadyActive
	}
	if err := canTransition(r.Status, StatusActive); err != nil {
		return Request{}, err
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	r.Status = StatusActive
	r.TenantID = tenantID
	r.ActivatedAt = now.UTC()
	return r, nil
}

// Issuer mints and verifies HMAC-signed verification tokens. The
// canonical token shape is "<id>.<exp>.<sig>" so it is grep-friendly
// and survives URL-safe transport.
type Issuer struct {
	secret []byte
}

// NewIssuer validates the secret length and returns an Issuer.
func NewIssuer(secret []byte) (*Issuer, error) {
	if len(secret) < MinHMACSecretBytes {
		return nil, fmt.Errorf("%w: got %d bytes, need >= %d", ErrSecretTooShort, len(secret), MinHMACSecretBytes)
	}
	return &Issuer{secret: append([]byte(nil), secret...)}, nil
}

// IssueToken returns the canonical verification token for r.
func (i *Issuer) IssueToken(r Request) (string, error) {
	if r.ID == "" {
		return "", fmt.Errorf("%w: id missing", ErrTokenInvalid)
	}
	if r.ExpiresAt.IsZero() {
		return "", fmt.Errorf("%w: expires_at missing", ErrTokenInvalid)
	}
	exp := strconv.FormatInt(r.ExpiresAt.UTC().Unix(), 10)
	sig := i.sign(r.ID, exp, r.Email)
	return fmt.Sprintf("%s.%s.%s", r.ID, exp, sig), nil
}

// VerifyToken parses and validates a verification token. Returns the
// embedded request id when the signature matches; ErrTokenInvalid or
// ErrTokenExpired otherwise.
func (i *Issuer) VerifyToken(token string, now time.Time, expectedEmail string) (string, error) {
	parts := strings.Split(strings.TrimSpace(token), ".")
	if len(parts) != 3 {
		return "", fmt.Errorf("%w: malformed", ErrTokenInvalid)
	}
	id, exp, sig := parts[0], parts[1], parts[2]
	if id == "" || exp == "" || sig == "" {
		return "", fmt.Errorf("%w: empty field", ErrTokenInvalid)
	}
	want := i.sign(id, exp, expectedEmail)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return "", ErrTokenInvalid
	}
	expUnix, err := strconv.ParseInt(exp, 10, 64)
	if err != nil {
		return "", fmt.Errorf("%w: bad exp", ErrTokenInvalid)
	}
	if now.IsZero() {
		now = time.Now().UTC()
	}
	if !now.UTC().Before(time.Unix(expUnix, 0).UTC()) {
		return "", ErrTokenExpired
	}
	return id, nil
}

func (i *Issuer) sign(id, exp, email string) string {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(id))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(exp))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(strings.ToLower(strings.TrimSpace(email))))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// looksLikeEmail is a deliberately narrow syntactic check; we don't
// pretend to validate RFC 5322. Adapters that want stricter checks
// can wrap this.
func looksLikeEmail(s string) bool {
	if s == "" {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	if strings.Contains(s, " ") {
		return false
	}
	return strings.Contains(s[at+1:], ".")
}

// slugPattern enforces kebab-case so registration cannot pass a slug
// the v2.4.0 tenant aggregate would reject. Keep this in sync with
// internal/tenant.IsValidSlug.
//
// v6.1.0 CF-12 follow-on (Sentrux complex_fn budget): decomposed the
// per-rune validator into three small helpers so the dispatcher loop
// stays cyclomatic 2 and the complete function clears the >=15 gate
// Sentrux uses to count "complex_fn" entries.
func slugPattern(s string) bool {
	if len(s) < 2 {
		return false
	}
	runes := []rune(s)
	for i, r := range runes {
		if !slugRuneAllowed(r, i, len(runes)) {
			return false
		}
	}
	return true
}

// slugRuneAllowed returns true when r is valid at position i of an
// n-rune slug. The three branches mirror the original switch
// (first/last/middle); pulling them apart lets each helper stay
// cyclomatic 1.
func slugRuneAllowed(r rune, i, n int) bool {
	switch {
	case i == 0:
		return slugStartRune(r)
	case i == n-1:
		return slugEndRune(r)
	default:
		return slugMiddleRune(r)
	}
}

// slugStartRune accepts a-z only -- a leading digit or hyphen would
// produce a slug the tenant aggregate would reject.
func slugStartRune(r rune) bool { return r >= 'a' && r <= 'z' }

// slugEndRune accepts a-z and 0-9 -- a trailing hyphen is rejected.
func slugEndRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9')
}

// slugMiddleRune accepts a-z, 0-9, and the kebab '-' separator.
func slugMiddleRune(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-'
}

// newID returns a 16-byte hex-encoded identifier sourced from
// crypto/rand.
func newID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("registration: rand: %w", err)
	}
	return hex.EncodeToString(buf), nil
}
