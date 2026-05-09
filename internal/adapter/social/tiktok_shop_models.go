package social

import (
	"context"
	"time"
)

// MinTikTokSecretBytes is the minimum length we accept for any
// TikTok Shop secret (client_secret, webhook_secret). Mirrors the
// internal/billing.MinWebhookSecretBytes floor (32 bytes) so the
// whole codebase has one number.
const MinTikTokSecretBytes = 32

// DefaultTikTokWebhookTolerance is the maximum age (now - timestamp)
// we accept for a TikTok Shop webhook signature. Matches Stripe's
// five-minute floor used by internal/billing.WebhookVerifier so
// operators only have one knob to tune.
const DefaultTikTokWebhookTolerance = 5 * time.Minute

// DefaultTikTokTimeout is the per-request HTTP timeout used by the
// TikTok Shop client. Conservative; the agent layer can shorten via
// ctx.WithTimeout for chatty paths.
const DefaultTikTokTimeout = 15 * time.Second

// DefaultTikTokBaseURL points at the TikTok Shop seller API root.
// Tests substitute a httptest server URL so cassettes never reach
// the live endpoint.
const DefaultTikTokBaseURL = "https://open-api.tiktokglobalshop.com"

// TikTokProductPayload is the canonical product payload the listing
// agent forwards to TikTok Shop. The TikTok-side wire envelope
// mapping is owned by the client; this struct stays narrow so the
// EC-3-2 agent does not couple to the HTTP layer.
type TikTokProductPayload struct {
	TenantID         string
	ExternalID       string // local product id; round-tripped via passthrough
	Title            string
	Description      string
	CategoryID       string
	BrandName        string
	PriceCents       int
	Currency         string
	StockUnits       int
	ShippingTemplate string
	Images           []string
	VideoSKUURL      string
	SellerSKU        string
	WarehouseID      string
}

// TikTokProduct is the subset of the TikTok Shop product response we
// surface to callers. Listing pagination uses cursor + page_size to
// mirror the official wire format.
type TikTokProduct struct {
	ID         string    `json:"product_id"`
	TenantID   string    `json:"tenant_id"`
	Title      string    `json:"title"`
	CategoryID string    `json:"category_id"`
	Status     string    `json:"status"`
	UpdatedAt  time.Time `json:"updated_at"`
	Stock      int       `json:"stock"`
	PriceCents int       `json:"price_cents"`
	Currency   string    `json:"currency"`
}

// TikTokProductPage is one page of TikTokProduct; the cursor is
// echoed back from the server when more pages exist.
type TikTokProductPage struct {
	Products  []TikTokProduct `json:"products"`
	NextPage  string          `json:"next_page,omitempty"`
	TotalSeen int             `json:"total_seen"`
}

// TikTokListProductsRequest controls the ListProducts call.
type TikTokListProductsRequest struct {
	TenantID string
	PageSize int
	Page     string // cursor returned by previous call; empty starts fresh
}

// TikTokOrder mirrors the canonical order shape the webhook layer
// emits onto the eventbus. Fields chosen to match the EC-3-3
// acceptance criterion (tenant scoped, deduplicable on order id).
type TikTokOrder struct {
	OrderID        string
	TenantID       string
	ShopID         string
	BuyerEmail     string
	TotalCents     int
	Currency       string
	Items          []TikTokOrderLine
	Status         string
	OccurredAt     time.Time
	IdempotencyKey string
}

// TikTokOrderLine is one line item inside a TikTokOrder.
type TikTokOrderLine struct {
	SKU         string
	Quantity    int
	UnitCents   int
	ProductID   string
	WarehouseID string
}

// TikTokInventoryUpdate is the unit of work consumed by the EC-3-4
// inventory sync saga. Tenant-scoped and dedup-keyed on order id.
type TikTokInventoryUpdate struct {
	TenantID       string
	ProductID      string
	SKU            string
	Delta          int    // negative for stock decrement
	OrderID        string // dedup key (cross-channel)
	IdempotencyKey string
	OccurredAt     time.Time
}

// Client is the EC-3-1 TikTok Shop seller API port. The EC-3-2
// listing agent + EC-3-4 inventory sync depend on this interface
// rather than the concrete *TikTokShopClient so tests can wire a
// fake without bringing the HTTP stack.
type Client interface {
	// ListProducts returns one page of products (cursor-based).
	ListProducts(ctx context.Context, req TikTokListProductsRequest) (TikTokProductPage, error)
	// CreateProduct publishes a product to TikTok Shop. Returns the
	// remote product id on success.
	CreateProduct(ctx context.Context, payload TikTokProductPayload) (string, error)
	// UpdateProduct updates an existing product. Returns the remote
	// product id on success (echoes payload's external -> remote map).
	UpdateProduct(ctx context.Context, remoteID string, payload TikTokProductPayload) error
	// DeleteProduct removes a remote listing. Idempotent on 404.
	DeleteProduct(ctx context.Context, remoteID string) error
	// SyncInventory applies a stock delta to the remote SKU.
	SyncInventory(ctx context.Context, update TikTokInventoryUpdate) error
	// Close releases resources. Implements lifecycle.Closer.
	Close(ctx context.Context) error
}
