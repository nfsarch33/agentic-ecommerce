package port

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
)

// ErrDigitalProductNotFound is returned when a DigitalProduct cannot be
// located.
var ErrDigitalProductNotFound = errors.New("digital product not found")

// ErrLicenseNotFound is returned when a License cannot be located.
var ErrLicenseNotFound = errors.New("license not found")

// ErrAccessGrantNotFound is returned when an AccessGrant cannot be
// located.
var ErrAccessGrantNotFound = errors.New("access grant not found")

// DigitalProductList is the paginated result for DigitalProduct
// listings.
type DigitalProductList struct {
	Products []digital.DigitalProduct
	Total    int
}

// LicenseList is the paginated result for License listings.
type LicenseList struct {
	Licenses []digital.License
	Total    int
}

// AccessGrantList is the paginated result for AccessGrant listings.
type AccessGrantList struct {
	Grants []digital.AccessGrant
	Total  int
}

// DigitalProductRepository persists DigitalProduct rows. Every method
// is tenant-aware so adapters can never accidentally cross tenant
// boundaries.
type DigitalProductRepository interface {
	Create(ctx context.Context, tenantID string, p digital.DigitalProduct) error
	Update(ctx context.Context, tenantID string, p digital.DigitalProduct) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.DigitalProduct, error)
	List(ctx context.Context, tenantID string, page, perPage int) (DigitalProductList, error)
	Delete(ctx context.Context, tenantID string, id uuid.UUID) error
}

// LicenseRepository persists License rows. SaveState is the optimistic
// state-machine-safe writer; adapters MUST short-circuit when the
// in-memory state matches the persisted one.
type LicenseRepository interface {
	Create(ctx context.Context, tenantID string, lic digital.License) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.License, error)
	List(ctx context.Context, tenantID string, page, perPage int) (LicenseList, error)
	ListByCustomer(ctx context.Context, tenantID string, customerID uuid.UUID, page, perPage int) (LicenseList, error)
	SaveState(ctx context.Context, tenantID string, lic digital.License) error
}

// AccessGrantRepository persists AccessGrant rows. Upsert MUST be
// idempotent on (tenant_id, customer_id, product_id) so re-purchases
// do not create duplicate grants.
type AccessGrantRepository interface {
	Upsert(ctx context.Context, tenantID string, grant digital.AccessGrant) error
	Get(ctx context.Context, tenantID string, id uuid.UUID) (digital.AccessGrant, error)
	ListByCustomer(ctx context.Context, tenantID string, customerID uuid.UUID, page, perPage int) (AccessGrantList, error)
	GetByCustomerProduct(ctx context.Context, tenantID string, customerID, productID uuid.UUID) (digital.AccessGrant, error)
}

// DownloadTokenIssuer mints time-limited signed URLs that customers
// hand back to the download endpoint. The interface intentionally
// stays narrow: implementations encapsulate the secret and the URL
// shape.
type DownloadTokenIssuer interface {
	// Issue mints a signed URL for the given (tenant, license,
	// product). The returned URL is opaque to callers; the bytes
	// commit to the signed payload via HMAC.
	Issue(req IssueDownloadRequest) (IssueDownloadResponse, error)

	// Verify decodes a previously-issued URL. It returns
	// ErrInvalidLicense when the signature is wrong, ErrTokenExpired
	// when the URL has aged out, or ErrTenantMismatch when the URL
	// references a different tenant.
	Verify(rawURL string, now time.Time) (DownloadClaims, error)
}

// IssueDownloadRequest is the input to DownloadTokenIssuer.Issue.
type IssueDownloadRequest struct {
	TenantID    string
	LicenseID   uuid.UUID
	ProductID   uuid.UUID
	IssuedAt    time.Time
	TTL         time.Duration
	UsesAllowed int
}

// IssueDownloadResponse carries the public URL and the structured
// download token row that callers persist alongside the licence.
type IssueDownloadResponse struct {
	URL   string
	Token digital.DownloadToken
}

// DownloadClaims carries the parsed payload of a verified download
// URL. Handlers use these to look up the licence and increment the
// use counter.
type DownloadClaims struct {
	TenantID  string
	LicenseID uuid.UUID
	ProductID uuid.UUID
	ExpiresAt time.Time
}

// DigitalAccessGrantor is the integration seam between order placement
// and the digital bounded context. Order handlers call GrantAccess
// once an order with digital line items has been persisted; the
// implementation creates a Licence + AccessGrant pair in the same
// transaction and publishes the lifecycle events.
type DigitalAccessGrantor interface {
	GrantAccess(ctx context.Context, req GrantAccessRequest) (GrantAccessResult, error)
}

// GrantAccessRequest carries the (customer, products) pair an order
// represents. CustomerID is the buyer.
type GrantAccessRequest struct {
	TenantID   string
	CustomerID uuid.UUID
	ProductIDs []uuid.UUID
	Source     digital.Source
	Now        time.Time
}

// GrantAccessResult enumerates the licences minted by the call.
type GrantAccessResult struct {
	Licenses []digital.License
	Grants   []digital.AccessGrant
}
