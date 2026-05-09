// File scope: v3.4.0 EC-4-2 Facebook Shop domain models.
//
// The models stay narrow and HTTP-free so the EC-4-3 channel router
// can build a payload without coupling to net/http. The wire DTOs
// (snake_case Graph API shapes) live in facebook_shop_client.go.
//
// Cite skill: go-clean-architecture (port + adapter; the
// FacebookClient interface lives here so the agent layer is free of
// HTTP types).
package social

import (
	"context"
	"time"
)

// MinFacebookSecretBytes is the minimum length we accept for the
// app secret. Same 32-byte floor as the TikTok pattern so the whole
// codebase has one number.
const MinFacebookSecretBytes = 32

// DefaultFacebookTimeout is the per-request HTTP timeout. Conservative
// to absorb the worst-case Graph API latency observed in the META
// docs (P95 ~3s on bulk catalog imports).
const DefaultFacebookTimeout = 30 * time.Second

// DefaultFacebookBaseURL points at the Graph API root. The minor
// version is pinned to v21.0 per the v3.4.0 EC-4-2 acceptance
// criterion. Tests substitute a httptest server URL so cassettes
// never reach the live endpoint.
const DefaultFacebookBaseURL = "https://graph.facebook.com/v21.0"

// MaxFacebookBatchSize caps a single /<catalog_id>/batch call. META
// hard limit is 50 product writes per /batch envelope; the v3.4.0
// acceptance criterion ("100 products in single batch" from the
// operator's perspective) is satisfied by the client chunking under
// the hood and round-tripping a single CreateProductBatch call.
const MaxFacebookBatchSize = 50

// FacebookProductPayload is the canonical product payload the
// channel agent forwards to META Commerce Manager. Fields chosen
// to match the Graph API "Product" object (retailer_id, name,
// description, price, availability, currency, image_url,
// brand). Tenant scoping is mandatory.
type FacebookProductPayload struct {
	TenantID         string
	RetailerID       string // local product id; round-tripped via passthrough
	Name             string
	Description      string
	CategoryID       string
	BrandName        string
	PriceCents       int
	Currency         string
	StockUnits       int
	Availability     string // "in stock" | "out of stock" | "preorder"
	Condition        string // "new" | "refurbished" | "used"
	ImageURL         string
	AdditionalImages []string
	URL              string // canonical product URL on the storefront
	GTIN             string // optional GTIN/EAN
	MPN              string // optional manufacturer part number
}

// FacebookProductCreated is the result echoed back from a
// successful CreateProduct call. The remote ID is the Graph API
// product id (the "id" field on the response envelope).
type FacebookProductCreated struct {
	RemoteID   string
	RetailerID string
	OccurredAt time.Time
}

// FacebookBatchResult is the per-item outcome of a CreateProductBatch
// call. Order matches the input payload. Error is nil on success.
type FacebookBatchResult struct {
	RetailerID string
	RemoteID   string
	Error      error
}

// FacebookInventoryUpdate is the unit of work consumed by the
// inventory sync agent. Tenant-scoped.
type FacebookInventoryUpdate struct {
	TenantID       string
	RetailerID     string
	StockUnits     int
	Availability   string
	IdempotencyKey string
	OccurredAt     time.Time
}

// FacebookOrderStatusPush is the unit of work consumed by the
// order status propagation agent. Surface from the v3.4.0
// EC-4-2 acceptance criterion ("order status push" -- map a
// local order status into META's commerce order state machine).
type FacebookOrderStatusPush struct {
	TenantID       string
	OrderID        string
	Status         string // "in_progress" | "shipped" | "completed" | "cancelled"
	TrackingNumber string
	TrackingURL    string
	OccurredAt     time.Time
}

// FacebookClient is the EC-4-2 Facebook Shop META Commerce Manager
// client port. The EC-4-3 channel router + future EC-7-4 status
// propagation depend on this interface rather than the concrete
// *FacebookShopClient so tests can wire a fake without bringing
// the HTTP stack.
type FacebookClient interface {
	// CreateProduct publishes a single product to a Catalogue.
	CreateProduct(ctx context.Context, payload FacebookProductPayload) (FacebookProductCreated, error)
	// CreateProductBatch publishes up to 100 products in a single
	// operator-visible call. The client chunks under MaxFacebookBatchSize
	// transparently. Order is preserved.
	CreateProductBatch(ctx context.Context, payloads []FacebookProductPayload) ([]FacebookBatchResult, error)
	// SyncInventory upserts the retailer-id stock + availability.
	SyncInventory(ctx context.Context, update FacebookInventoryUpdate) error
	// PushOrderStatus pushes a local fulfilment status into the
	// Commerce Manager order state machine.
	PushOrderStatus(ctx context.Context, push FacebookOrderStatusPush) error
	// Close releases resources. Implements lifecycle.Closer.
	Close(ctx context.Context) error
}

// FacebookMetricsHook is the small port the EC-4-2 client uses to
// emit Prometheus counters / histograms without coupling to the
// internal/metrics.Registry. The observability spine implements
// this interface.
//
// Every method is nil-safe via the *FacebookShopClient.recordAPI
// guard so cmd/* binaries that disable metrics in dev / unit tests
// can pass nil without extra branching.
type FacebookMetricsHook interface {
	RecordAPICall(tenantID, endpoint, status string, durationSeconds float64)
	RecordSignatureFailure(tenantID, reason string)
}
