package digital_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/inmemory"
	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/signedurl"
	"github.com/nfsarch33/agentic-ecommerce/internal/digital"
	digitaldomain "github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

type recordingPublisher struct {
	events []eventbus.Event
}

func (p *recordingPublisher) Publish(_ context.Context, evt eventbus.Event) error {
	p.events = append(p.events, evt)
	return nil
}

func newTestService(t *testing.T, clock digital.Clock) (*digital.Service, *inmemory.DigitalProductRepository, *recordingPublisher, port.DownloadTokenIssuer) {
	t.Helper()
	products := inmemory.NewDigitalProductRepository()
	licenses := inmemory.NewLicenseRepository()
	grants := inmemory.NewAccessGrantRepository()
	keys, err := digitaldomain.NewHMACLicenseKeyGenerator(make([]byte, 32))
	if err != nil {
		t.Fatalf("NewHMACLicenseKeyGenerator: %v", err)
	}
	issuer, err := signedurl.New(signedurl.Config{
		BaseURL: "https://cdn.example.com/api/v1/digital-downloads",
		Secret:  []byte("test-secret-32-bytes-of-signed-url-1!"),
	})
	if err != nil {
		t.Fatalf("signedurl.New: %v", err)
	}
	pub := &recordingPublisher{}
	svc, err := digital.New(digital.Config{
		Products:  products,
		Licenses:  licenses,
		Grants:    grants,
		Keys:      keys,
		Issuer:    issuer,
		Publisher: pub,
		Clock:     clock,
		Source:    "test.digital",
	})
	if err != nil {
		t.Fatalf("digital.New: %v", err)
	}
	return svc, products, pub, issuer
}

func seedProduct(t *testing.T, repo *inmemory.DigitalProductRepository, tenantID, sku string, now time.Time) digitaldomain.DigitalProduct {
	t.Helper()
	p, err := digitaldomain.NewDigitalProduct(digitaldomain.DigitalProductInput{
		TenantID: tenantID,
		SKU:      sku,
		Name:     "Sample",
		FilePath: "tenant/digital/x.pdf",
		FileSize: 1024,
		Version:  "1",
		Now:      now,
	})
	if err != nil {
		t.Fatalf("NewDigitalProduct: %v", err)
	}
	if err := repo.Create(context.Background(), tenantID, p); err != nil {
		t.Fatalf("repo.Create: %v", err)
	}
	return p
}

func TestServiceIssueLicensePublishesActivationEvent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, products, pub, _ := newTestService(t, func() time.Time { return now })
	product := seedProduct(t, products, "tenant-a", "PDF-001", now)
	customer := uuid.New()

	res, err := svc.IssueLicense(context.Background(), digital.IssueLicenseRequest{
		TenantID:   "tenant-a",
		CustomerID: customer,
		ProductID:  product.ID(),
		Source:     digitaldomain.SourceAdmin,
	})
	if err != nil {
		t.Fatalf("IssueLicense: %v", err)
	}
	if res.License.State() != digitaldomain.StateActive {
		t.Fatalf("state = %q", res.License.State())
	}
	if res.Grant.LicenseID() != res.License.ID() {
		t.Fatalf("grant license = %v, want %v", res.Grant.LicenseID(), res.License.ID())
	}
	if len(pub.events) != 1 {
		t.Fatalf("events = %d, want 1 (license.activated only)", len(pub.events))
	}
	if pub.events[0].Type != eventbus.LicenseActivated {
		t.Fatalf("event type = %s", pub.events[0].Type)
	}
}

func TestServiceIssueLicenseEmitsPurchasedWhenSourceIsPurchase(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, products, pub, _ := newTestService(t, func() time.Time { return now })
	product := seedProduct(t, products, "tenant-a", "PDF-001", now)
	if _, err := svc.IssueLicense(context.Background(), digital.IssueLicenseRequest{
		TenantID:   "tenant-a",
		CustomerID: uuid.New(),
		ProductID:  product.ID(),
		Source:     digitaldomain.SourcePurchase,
	}); err != nil {
		t.Fatalf("IssueLicense: %v", err)
	}
	if len(pub.events) != 2 {
		t.Fatalf("events = %d, want 2 (license.activated + digital.purchased)", len(pub.events))
	}
	types := []eventbus.EventType{pub.events[0].Type, pub.events[1].Type}
	hasActivated := types[0] == eventbus.LicenseActivated || types[1] == eventbus.LicenseActivated
	hasPurchased := types[0] == eventbus.DigitalPurchased || types[1] == eventbus.DigitalPurchased
	if !hasActivated || !hasPurchased {
		t.Fatalf("expected both activation and purchase events, got %v", types)
	}
}

func TestServiceRevokeLicenseTransitionsState(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, products, pub, _ := newTestService(t, func() time.Time { return now })
	product := seedProduct(t, products, "tenant-a", "PDF-001", now)
	res, err := svc.IssueLicense(context.Background(), digital.IssueLicenseRequest{
		TenantID:   "tenant-a",
		CustomerID: uuid.New(),
		ProductID:  product.ID(),
		Source:     digitaldomain.SourcePurchase,
	})
	if err != nil {
		t.Fatalf("IssueLicense: %v", err)
	}
	pub.events = nil
	revoked, err := svc.RevokeLicense(context.Background(), "tenant-a", res.License.ID())
	if err != nil {
		t.Fatalf("RevokeLicense: %v", err)
	}
	if revoked.State() != digitaldomain.StateRevoked {
		t.Fatalf("state = %q", revoked.State())
	}
	// Repeat revoke is illegal.
	if _, err := svc.RevokeLicense(context.Background(), "tenant-a", res.License.ID()); !errors.Is(err, digitaldomain.ErrInvalidTransition) {
		t.Fatalf("repeat revoke err = %v, want ErrInvalidTransition", err)
	}
	// One license.revoked event was published.
	if len(pub.events) != 1 || pub.events[0].Type != eventbus.LicenseRevoked {
		t.Fatalf("publish stream = %v, want one license.revoked", pub.events)
	}
}

func TestServiceIssueDownloadHonoursCustomerBoundary(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, products, _, issuer := newTestService(t, func() time.Time { return now })
	product := seedProduct(t, products, "tenant-a", "PDF-001", now)
	owner := uuid.New()
	res, err := svc.IssueLicense(context.Background(), digital.IssueLicenseRequest{
		TenantID:   "tenant-a",
		CustomerID: owner,
		ProductID:  product.ID(),
		Source:     digitaldomain.SourcePurchase,
	})
	if err != nil {
		t.Fatalf("IssueLicense: %v", err)
	}
	out, _, err := svc.IssueDownload(context.Background(), "tenant-a", res.License.ID(), owner, time.Minute, 1)
	if err != nil {
		t.Fatalf("IssueDownload: %v", err)
	}
	if out.URL == "" {
		t.Fatal("URL empty")
	}
	// Validate the URL parses through the issuer.
	if _, err := issuer.Verify(out.URL, now.Add(time.Second)); err != nil {
		t.Fatalf("issuer.Verify: %v", err)
	}
	// Wrong customer must be rejected.
	other := uuid.New()
	if _, _, err := svc.IssueDownload(context.Background(), "tenant-a", res.License.ID(), other, time.Minute, 1); !errors.Is(err, digitaldomain.ErrTenantMismatch) {
		t.Fatalf("cross-customer download = %v, want ErrTenantMismatch", err)
	}
}

func TestServiceIssueDownloadRejectsRevokedLicense(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, products, _, _ := newTestService(t, func() time.Time { return now })
	product := seedProduct(t, products, "tenant-a", "PDF-001", now)
	res, err := svc.IssueLicense(context.Background(), digital.IssueLicenseRequest{
		TenantID:   "tenant-a",
		CustomerID: uuid.New(),
		ProductID:  product.ID(),
		Source:     digitaldomain.SourceAdmin,
	})
	if err != nil {
		t.Fatalf("IssueLicense: %v", err)
	}
	if _, err := svc.RevokeLicense(context.Background(), "tenant-a", res.License.ID()); err != nil {
		t.Fatalf("RevokeLicense: %v", err)
	}
	if _, _, err := svc.IssueDownload(context.Background(), "tenant-a", res.License.ID(), res.License.CustomerID(), time.Minute, 1); !errors.Is(err, digitaldomain.ErrLicenseRevoked) {
		t.Fatalf("download after revoke = %v, want ErrLicenseRevoked", err)
	}
}

func TestServiceGrantAccessIteratesProducts(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, products, _, _ := newTestService(t, func() time.Time { return now })
	p1 := seedProduct(t, products, "tenant-a", "PDF-A", now)
	p2 := seedProduct(t, products, "tenant-a", "PDF-B", now)
	res, err := svc.GrantAccess(context.Background(), port.GrantAccessRequest{
		TenantID:   "tenant-a",
		CustomerID: uuid.New(),
		ProductIDs: []uuid.UUID{p1.ID(), p2.ID()},
		Source:     digitaldomain.SourcePurchase,
	})
	if err != nil {
		t.Fatalf("GrantAccess: %v", err)
	}
	if len(res.Licenses) != 2 || len(res.Grants) != 2 {
		t.Fatalf("licences/grants = %d/%d, want 2/2", len(res.Licenses), len(res.Grants))
	}
}

func TestServiceTenantRequiredEverywhere(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 8, 12, 0, 0, 0, time.UTC)
	svc, _, _, _ := newTestService(t, func() time.Time { return now })
	if _, err := svc.IssueLicense(context.Background(), digital.IssueLicenseRequest{}); !errors.Is(err, digitaldomain.ErrTenantRequired) {
		t.Fatalf("IssueLicense empty tenant: %v", err)
	}
	if _, err := svc.RevokeLicense(context.Background(), "", uuid.New()); !errors.Is(err, digitaldomain.ErrTenantRequired) {
		t.Fatalf("RevokeLicense empty tenant: %v", err)
	}
	if _, _, err := svc.IssueDownload(context.Background(), "", uuid.New(), uuid.New(), time.Minute, 1); !errors.Is(err, digitaldomain.ErrTenantRequired) {
		t.Fatalf("IssueDownload empty tenant: %v", err)
	}
}
