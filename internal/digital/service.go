// Package digital wires the digital bounded context use cases on top
// of the repository ports. The orchestration logic lives here so the
// HTTP handlers stay thin and so the order-flow integration in v2.4.0
// can call exactly the same code path (synchronously today, via a
// Temporal activity later).
package digital

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	digitaldomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// EventPublisher is the narrow seam the service uses to emit
// digital.* / license.* events. The in-memory eventbus.Bus satisfies
// this; tests can inject a recording fake.
type EventPublisher interface {
	Publish(ctx context.Context, evt eventbus.Event) error
}

// Clock returns the current time. Injected so tests stay
// deterministic.
type Clock func() time.Time

// Service is the digital bounded context's use-case layer. It owns
// the licence + access-grant lifecycle and emits domain events.
type Service struct {
	products  port.DigitalProductRepository
	licenses  port.LicenseRepository
	grants    port.AccessGrantRepository
	keys      digitaldomain.LicenseKeyGenerator
	issuer    port.DownloadTokenIssuer
	publisher EventPublisher
	clock     Clock
	source    string
}

// Config configures a Service. Required fields: Products, Licenses,
// Grants, Keys.
type Config struct {
	Products  port.DigitalProductRepository
	Licenses  port.LicenseRepository
	Grants    port.AccessGrantRepository
	Keys      digitaldomain.LicenseKeyGenerator
	Issuer    port.DownloadTokenIssuer
	Publisher EventPublisher
	Clock     Clock
	Source    string
}

// New constructs a Service. Returns an error when required dependencies
// are missing.
func New(cfg Config) (*Service, error) {
	if cfg.Products == nil {
		return nil, errors.New("digital service requires a DigitalProductRepository")
	}
	if cfg.Licenses == nil {
		return nil, errors.New("digital service requires a LicenseRepository")
	}
	if cfg.Grants == nil {
		return nil, errors.New("digital service requires an AccessGrantRepository")
	}
	if cfg.Keys == nil {
		return nil, errors.New("digital service requires a LicenseKeyGenerator")
	}
	clock := cfg.Clock
	if clock == nil {
		clock = func() time.Time { return time.Now().UTC() }
	}
	source := cfg.Source
	if source == "" {
		source = "mc-api.digital"
	}
	return &Service{
		products:  cfg.Products,
		licenses:  cfg.Licenses,
		grants:    cfg.Grants,
		keys:      cfg.Keys,
		issuer:    cfg.Issuer,
		publisher: cfg.Publisher,
		clock:     clock,
		source:    source,
	}, nil
}

// IssueLicense mints a licence + access grant for (customer, product)
// and emits the digital.purchased + license.activated events.
//
// IssueLicense is the integration point the order-completion path
// will call once a Temporal order workflow exists; today the admin
// endpoints invoke it directly.
type IssueLicenseRequest struct {
	TenantID       string
	CustomerID     uuid.UUID
	ProductID      uuid.UUID
	Source         digitaldomain.Source
	MaxActivations int
	ExpiresAt      time.Time
}

// IssueLicenseResult enumerates the artefacts created by IssueLicense.
type IssueLicenseResult struct {
	License digitaldomain.License
	Grant   digitaldomain.AccessGrant
	Product digitaldomain.DigitalProduct
}

// IssueLicense is the canonical entry point for granting digital
// access. It validates the product exists, mints a key, persists the
// licence, upserts an access grant, and publishes the lifecycle
// events.
func (s *Service) IssueLicense(ctx context.Context, req IssueLicenseRequest) (IssueLicenseResult, error) {
	if req.TenantID == "" {
		return IssueLicenseResult{}, digitaldomain.ErrTenantRequired
	}
	product, err := s.products.Get(ctx, req.TenantID, req.ProductID)
	if err != nil {
		return IssueLicenseResult{}, err
	}
	now := s.clock()
	key, err := s.keys.Generate(req.TenantID, []byte(product.SKU()+":"+req.CustomerID.String()))
	if err != nil {
		return IssueLicenseResult{}, fmt.Errorf("license key: %w", err)
	}
	lic, err := digitaldomain.NewLicense(digitaldomain.LicenseInput{
		TenantID:       req.TenantID,
		ProductID:      product.ID(),
		CustomerID:     req.CustomerID,
		Key:            key,
		MaxActivations: req.MaxActivations,
		ExpiresAt:      req.ExpiresAt,
		Now:            now,
	})
	if err != nil {
		return IssueLicenseResult{}, err
	}
	if err := s.licenses.Create(ctx, req.TenantID, lic); err != nil {
		return IssueLicenseResult{}, err
	}
	source := req.Source
	if source == "" {
		source = digitaldomain.SourceAdmin
	}
	grant, err := digitaldomain.NewAccessGrant(digitaldomain.AccessGrantInput{
		TenantID:   req.TenantID,
		CustomerID: req.CustomerID,
		ProductID:  product.ID(),
		LicenseID:  lic.ID(),
		Source:     source,
		Now:        now,
	})
	if err != nil {
		return IssueLicenseResult{}, err
	}
	if err := s.grants.Upsert(ctx, req.TenantID, grant); err != nil {
		return IssueLicenseResult{}, err
	}
	s.publish(ctx, eventbus.LicenseActivated, lic, product, grant.Source(), now)
	if source == digitaldomain.SourcePurchase {
		s.publish(ctx, eventbus.DigitalPurchased, lic, product, source, now)
	}
	return IssueLicenseResult{License: lic, Grant: grant, Product: product}, nil
}

// RevokeLicense moves a licence from active -> revoked and publishes
// license.revoked. It is idempotent: revoking a revoked licence
// returns digital.ErrInvalidTransition wrapped through Apply.
func (s *Service) RevokeLicense(ctx context.Context, tenantID string, licenseID uuid.UUID) (digitaldomain.License, error) {
	if tenantID == "" {
		return digitaldomain.License{}, digitaldomain.ErrTenantRequired
	}
	lic, err := s.licenses.Get(ctx, tenantID, licenseID)
	if err != nil {
		return digitaldomain.License{}, err
	}
	now := s.clock()
	if err := lic.Apply(digitaldomain.TransitionRevoke, now); err != nil {
		return digitaldomain.License{}, err
	}
	if err := s.licenses.SaveState(ctx, tenantID, lic); err != nil {
		return digitaldomain.License{}, err
	}
	product, perr := s.products.Get(ctx, tenantID, lic.ProductID())
	if perr == nil {
		s.publish(ctx, eventbus.LicenseRevoked, lic, product, "", now)
	}
	return lic, nil
}

// IssueDownload mints a signed URL after validating the licence is
// usable for the supplied customer.
func (s *Service) IssueDownload(ctx context.Context, tenantID string, licenseID uuid.UUID, customerID uuid.UUID, ttl time.Duration, usesAllowed int) (port.IssueDownloadResponse, digitaldomain.License, error) {
	if tenantID == "" {
		return port.IssueDownloadResponse{}, digitaldomain.License{}, digitaldomain.ErrTenantRequired
	}
	if s.issuer == nil {
		return port.IssueDownloadResponse{}, digitaldomain.License{}, errors.New("download token issuer not configured")
	}
	lic, err := s.licenses.Get(ctx, tenantID, licenseID)
	if err != nil {
		return port.IssueDownloadResponse{}, digitaldomain.License{}, err
	}
	if customerID != uuid.Nil && lic.CustomerID() != customerID {
		return port.IssueDownloadResponse{}, digitaldomain.License{}, digitaldomain.ErrTenantMismatch
	}
	now := s.clock()
	if err := lic.CheckActive(now); err != nil {
		return port.IssueDownloadResponse{}, digitaldomain.License{}, err
	}
	res, err := s.issuer.Issue(port.IssueDownloadRequest{
		TenantID:    tenantID,
		LicenseID:   lic.ID(),
		ProductID:   lic.ProductID(),
		IssuedAt:    now,
		TTL:         ttl,
		UsesAllowed: usesAllowed,
	})
	if err != nil {
		return port.IssueDownloadResponse{}, digitaldomain.License{}, err
	}
	product, perr := s.products.Get(ctx, tenantID, lic.ProductID())
	if perr == nil {
		s.publish(ctx, eventbus.DigitalDownloaded, lic, product, "", now)
	}
	return res, lic, nil
}

// GrantAccess satisfies port.DigitalAccessGrantor for the future
// order-completion integration. It iterates ProductIDs and calls
// IssueLicense for each.
func (s *Service) GrantAccess(ctx context.Context, req port.GrantAccessRequest) (port.GrantAccessResult, error) {
	if req.TenantID == "" {
		return port.GrantAccessResult{}, digitaldomain.ErrTenantRequired
	}
	source := req.Source
	if source == "" {
		source = digitaldomain.SourcePurchase
	}
	out := port.GrantAccessResult{}
	for _, pid := range req.ProductIDs {
		res, err := s.IssueLicense(ctx, IssueLicenseRequest{
			TenantID:   req.TenantID,
			CustomerID: req.CustomerID,
			ProductID:  pid,
			Source:     source,
		})
		if err != nil {
			return port.GrantAccessResult{}, err
		}
		out.Licenses = append(out.Licenses, res.License)
		out.Grants = append(out.Grants, res.Grant)
	}
	return out, nil
}

func (s *Service) publish(ctx context.Context, eventType eventbus.EventType, lic digitaldomain.License, product digitaldomain.DigitalProduct, source digitaldomain.Source, now time.Time) {
	if s.publisher == nil {
		return
	}
	payload := eventbus.DigitalPayload{
		Version:    eventbus.DigitalPayloadVersion,
		TenantID:   lic.TenantID(),
		LicenseID:  lic.ID().String(),
		CustomerID: lic.CustomerID().String(),
		ProductID:  product.ID().String(),
		ProductSKU: product.SKU(),
		State:      string(lic.State()),
		Source:     string(source),
	}
	evt, err := eventbus.NewDigitalEvent(eventType, s.source, now, payload)
	if err != nil {
		return
	}
	_ = s.publisher.Publish(ctx, evt)
}
