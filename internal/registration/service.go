package registration

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/tenant"
)

// TenantProvisioner is the abstraction over the tenant aggregate
// service. The real one is *tenant.AggregateService; tests inject a
// fake.
type TenantProvisioner interface {
	Create(ctx context.Context, in tenant.CreateTenantInput) (tenant.Tenant, error)
	Activate(ctx context.Context, id tenant.ID) (tenant.Tenant, error)
}

// Notifier is the abstraction over the email/notification stub. The
// real one persists for inspection; production wiring publishes to
// the email/n8n adapter.
type Notifier interface {
	NotifyRegistrationRequested(ctx context.Context, req Request, token string) error
	NotifyRegistrationVerified(ctx context.Context, req Request) error
}

// Service orchestrates the public /register flow. It is the thing the
// /register handlers call.
type Service struct {
	repo     Repository
	issuer   *Issuer
	tenants  TenantProvisioner
	notifier Notifier
	now      func() time.Time
	ttl      time.Duration
}

// ServiceConfig configures a Service.
type ServiceConfig struct {
	Repository Repository
	Issuer     *Issuer
	Tenants    TenantProvisioner
	Notifier   Notifier
	Now        func() time.Time
	TokenTTL   time.Duration
}

// NewService validates the configuration and returns a Service.
func NewService(cfg ServiceConfig) (*Service, error) {
	if cfg.Repository == nil {
		return nil, fmt.Errorf("registration service: repository required")
	}
	if cfg.Issuer == nil {
		return nil, fmt.Errorf("registration service: issuer required")
	}
	if cfg.Tenants == nil {
		return nil, fmt.Errorf("registration service: tenant provisioner required")
	}
	now := cfg.Now
	if now == nil {
		now = func() time.Time { return time.Now().UTC() }
	}
	ttl := cfg.TokenTTL
	if ttl <= 0 {
		ttl = DefaultTokenTTL
	}
	return &Service{
		repo:     cfg.Repository,
		issuer:   cfg.Issuer,
		tenants:  cfg.Tenants,
		notifier: cfg.Notifier,
		now:      now,
		ttl:      ttl,
	}, nil
}

// SubmitOutput is the public response from Submit. Token is included
// for tests; production handlers redact it before responding.
type SubmitOutput struct {
	Request Request
	Token   string
}

// Submit creates a new RegistrationRequest, mints a verification
// token, and notifies the user. Idempotent on email when the prior
// row is still in pending state.
func (s *Service) Submit(ctx context.Context, in SubmitInput) (SubmitOutput, error) {
	if in.Now.IsZero() {
		in.Now = s.now()
	}
	req, err := NewRequest(in, s.ttl)
	if err != nil {
		return SubmitOutput{}, err
	}
	if err := s.repo.Create(ctx, req); err != nil {
		if errors.Is(err, ErrRequestAlreadyExists) {
			return SubmitOutput{}, fmt.Errorf("%w: email=%q", ErrSlugTaken, req.Email)
		}
		return SubmitOutput{}, err
	}
	token, err := s.issuer.IssueToken(req)
	if err != nil {
		return SubmitOutput{}, err
	}
	if s.notifier != nil {
		_ = s.notifier.NotifyRegistrationRequested(ctx, req, token)
	}
	return SubmitOutput{Request: req, Token: token}, nil
}

// Verify consumes a verification token. Returns the verified Request
// or ErrTokenInvalid / ErrTokenExpired.
func (s *Service) Verify(ctx context.Context, token string) (Request, error) {
	parts := splitToken(token)
	if parts == nil {
		return Request{}, ErrTokenInvalid
	}
	candidate, err := s.repo.Get(ctx, parts.ID)
	if err != nil {
		return Request{}, err
	}
	id, err := s.issuer.VerifyToken(token, s.now(), candidate.Email)
	if err != nil {
		return Request{}, err
	}
	if id != candidate.ID {
		return Request{}, ErrTokenInvalid
	}
	updated, err := candidate.MarkVerified(s.now())
	if err != nil {
		return Request{}, err
	}
	if err := s.repo.Save(ctx, updated); err != nil {
		return Request{}, err
	}
	if s.notifier != nil {
		_ = s.notifier.NotifyRegistrationVerified(ctx, updated)
	}
	return updated, nil
}

// CompleteOnboarding finishes the registration by provisioning a
// Tenant aggregate and marking the request active. The slug used is
// the one originally requested unless the caller overrides it.
func (s *Service) CompleteOnboarding(ctx context.Context, id, companyName, finalPlan string) (Request, tenant.Tenant, error) {
	req, err := s.repo.Get(ctx, id)
	if err != nil {
		return Request{}, tenant.Tenant{}, err
	}
	if req.Status == StatusPendingEmailVerification {
		return Request{}, tenant.Tenant{}, fmt.Errorf("%w: not yet verified", ErrInvalidTransition)
	}
	if req.Status == StatusActive {
		return Request{}, tenant.Tenant{}, ErrAlreadyActive
	}
	updated, err := req.MarkOnboarding(companyName, s.now())
	if err != nil {
		return Request{}, tenant.Tenant{}, err
	}
	plan := finalPlan
	if plan == "" {
		plan = updated.PlanRequested
	}
	t, err := s.tenants.Create(ctx, tenant.CreateTenantInput{
		Slug: updated.SlugRequested,
		Name: updated.CompanyName,
		Plan: plan,
	})
	if err != nil {
		if errors.Is(err, tenant.ErrTenantSlugAlreadyExists) {
			return Request{}, tenant.Tenant{}, fmt.Errorf("%w: %q", ErrSlugTaken, updated.SlugRequested)
		}
		return Request{}, tenant.Tenant{}, err
	}
	activated, err := s.tenants.Activate(ctx, t.ID)
	if err != nil {
		return Request{}, tenant.Tenant{}, err
	}
	final, err := updated.MarkActive(string(activated.ID), s.now())
	if err != nil {
		return Request{}, tenant.Tenant{}, err
	}
	if err := s.repo.Save(ctx, final); err != nil {
		return Request{}, tenant.Tenant{}, err
	}
	return final, activated, nil
}

// Get returns a registration request by id.
func (s *Service) Get(ctx context.Context, id string) (Request, error) {
	return s.repo.Get(ctx, id)
}

type tokenParts struct {
	ID  string
	Exp string
	Sig string
}

func splitToken(token string) *tokenParts {
	if token == "" {
		return nil
	}
	parts := []rune(token)
	dot := 0
	out := tokenParts{}
	last := 0
	for i, r := range parts {
		if r == '.' {
			seg := string(parts[last:i])
			switch dot {
			case 0:
				out.ID = seg
			case 1:
				out.Exp = seg
			default:
				return nil
			}
			dot++
			last = i + 1
		}
	}
	if dot != 2 {
		return nil
	}
	out.Sig = string(parts[last:])
	if out.ID == "" || out.Exp == "" || out.Sig == "" {
		return nil
	}
	return &out
}
