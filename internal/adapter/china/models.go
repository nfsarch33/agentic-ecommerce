// Package china exposes adapter clients for the Chinese B2B/B2C
// supplier platforms (1688, Taobao/Tmall) that drive the v3.1.0 EC-1
// China Sourcing Agent.
//
// Design notes:
//
//   - Adapter clients implement a small, platform-agnostic interface
//     so EC-1-3 sourcing agent can fan out across multiple platforms
//     concurrently via internal/workerpool without coupling to either
//     platform's wire format.
//   - All HTTP traffic flows through net/http with a custom
//     Transport so timeouts, retry, and rate-limit semantics are
//     observable and testable. We avoid chromedp for v3.1.0 because
//     a headless browser cannot be deterministically replayed in CI;
//     the chromedp-driven dynamic-JS fallback is a v3.1.x story that
//     can layer on top of this interface.
//   - Session cookies are sourced from environment variables
//     (ECOMMERCE_1688_SESSION_COOKIE, ECOMMERCE_TAOBAO_SESSION_COOKIE)
//     for v3.1.0. EC-10-1 will move them to the OS keychain.
//   - Rate limiting uses time.Ticker + a fixed token interval (1 req
//     per 2s for 1688) to avoid bot detection. Per the spec, this
//     replaces ad-hoc sleeps with deterministic behaviour.
//
// Cite skill: go-clean-architecture (port + adapter pattern; the
// Client interface is the port, Client1688 / TaobaoClient the
// adapters).
package china

import (
	"context"
	"errors"
	"time"
)

// Sentinels surfaced by the adapter layer. Tests + agent layer use
// errors.Is to branch on these.
var (
	// ErrAdapterUnconfigured is returned when an adapter is invoked
	// without a session cookie or base URL (i.e. zero-value client).
	ErrAdapterUnconfigured = errors.New("china: adapter unconfigured")

	// Err1688RateLimited is returned when 1688 returns 429 / blocks
	// us. Wrapped with %w from RetryWithBackoff so callers can
	// errors.Is.
	Err1688RateLimited = errors.New("china: 1688 rate-limited")

	// ErrTaobaoCategoryUnknown is returned when a Taobao response
	// references a category we do not yet have a mapping for. The
	// agent layer falls back to "unknown" rather than crashing.
	ErrTaobaoCategoryUnknown = errors.New("china: taobao category unknown")

	// ErrInvalidQuery is returned by Search when the query is empty.
	ErrInvalidQuery = errors.New("china: empty search query")
)

// Source is the upstream platform identifier mirrored by the
// compliance package. We keep the constants here too so the adapter
// layer can label its results without importing compliance.
type Source string

const (
	Source1688   Source = "1688"
	SourceTaobao Source = "taobao"
)

// Product is the canonical China-sourced product shape. The sourcing
// agent translates this into a domain.Supplier + a catalog product
// proposal downstream. Fields chosen to satisfy the EC-1-1 acceptance
// test (price, MOQ, supplier_rating populated).
type Product struct {
	ExternalID       string    `json:"external_id"`
	Source           Source    `json:"source"`
	Title            string    `json:"title"`
	Category         string    `json:"category"`
	SubCategory      string    `json:"sub_category,omitempty"`
	PriceCNYCents    int       `json:"price_cny_cents"`
	MOQ              int       `json:"moq"`
	LeadTimeDays     int       `json:"lead_time_days"`
	SupplierID       string    `json:"supplier_id"`
	SupplierName     string    `json:"supplier_name"`
	SupplierRating   float64   `json:"supplier_rating"`
	SupplierVerified bool      `json:"supplier_verified"`
	ReviewCount      int       `json:"review_count"`
	MonthlySalesUnit int       `json:"monthly_sales_unit,omitempty"`
	URL              string    `json:"url"`
	FetchedAt        time.Time `json:"fetched_at"`
}

// SearchRequest is the canonical search input used by both adapters.
type SearchRequest struct {
	Keyword      string
	MaxResults   int
	MinMOQ       int
	MaxMOQ       int
	MinRating    float64
	CategoryHint string
}

// ProductDetailRequest fetches a single product by its
// platform-specific external id (used by Taobao for product reviews).
type ProductDetailRequest struct {
	ExternalID string
}

// Client is the platform-agnostic supplier-data port consumed by the
// EC-1-3 sourcing agent. Both Client1688 and TaobaoClient implement
// this interface so the agent can fan out via workerpool without
// branching on platform.
type Client interface {
	Source() Source
	Search(ctx context.Context, req SearchRequest) ([]Product, error)
	ProductDetail(ctx context.Context, req ProductDetailRequest) (Product, error)
	Close(ctx context.Context) error
}
