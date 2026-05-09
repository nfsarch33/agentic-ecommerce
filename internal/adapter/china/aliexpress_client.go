// File scope: v3.5.0 EC-7-2 AliExpress adapter (FALLBACK ONLY).
//
// AliExpress is the secondary supplier path for the v3.5.0 EC-7-2
// drop-ship agent. The adapter is intentionally a MINIMAL STUB:
// the EC-7-2 acceptance only requires fallback-on-primary-failure
// behaviour, so this client implements just enough surface area to
// satisfy the SupplierOrderClient port. A full HTTP integration
// against AliExpress Open Platform is deferred to v3.6+ once the
// primary 1688/Taobao path is production-validated.
//
// Reuse evidence:
//   - The Client interface from china/models.go (Source / Search /
//     ProductDetail / Close) is the same shape as Client1688 +
//     TaobaoClient.
//   - The DefaultAliExpressBaseURL + DefaultAliExpressTimeout
//     constants follow the same naming pattern.
//
// This file's design rule: NEVER actually hit the AliExpress
// network in v3.5.0. Operator wiring sets ECOMMERCE_ALIEXPRESS_STUB=1
// (default) in production until v3.5.x flips the live path.
package china

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SourceAliExpress is the v3.5.0 EC-7-2 fallback source identifier.
const SourceAliExpress Source = "aliexpress"

// DefaultAliExpressBaseURL is the AliExpress Open Platform base URL.
// The v3.5.0 stub does NOT actually call this URL; the constant is
// here so the v3.5.x live integration can switch on it without code
// changes.
const DefaultAliExpressBaseURL = "https://api.aliexpress.com"

// DefaultAliExpressTimeout is the per-request HTTP timeout.
const DefaultAliExpressTimeout = 15 * time.Second

// AliExpressStubEnvVar is the env var that disables the live HTTP
// path. When set to "1" the adapter returns the stub response shape;
// production deployment in v3.5.0 sets this to "1".
const AliExpressStubEnvVar = "ECOMMERCE_ALIEXPRESS_STUB"

// ErrAliExpressStubMode is returned by Search / ProductDetail when
// the adapter is in stub mode AND callers ask for live data. The
// EC-7-2 drop-ship agent treats this as "primary supplier
// preferred" -- the adapter is only consulted for PlaceOrder which
// has its own deterministic-stub fallback.
var ErrAliExpressStubMode = errors.New("china: aliexpress adapter in stub mode")

// ConfigAliExpress controls AliExpressClient wiring.
type ConfigAliExpress struct {
	BaseURL    string
	APIKey     string
	HTTPClient *http.Client
	UserAgent  string
	StubMode   bool
}

// AliExpressClient is the v3.5.0 EC-7-2 fallback client.
type AliExpressClient struct {
	cfg    ConfigAliExpress
	logger *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewAliExpressClient constructs a fallback client.
func NewAliExpressClient(logger *slog.Logger, cfg ConfigAliExpress) (*AliExpressClient, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.BaseURL == "" {
		cfg.BaseURL = DefaultAliExpressBaseURL
	}
	if cfg.HTTPClient == nil {
		cfg.HTTPClient = &http.Client{Timeout: DefaultAliExpressTimeout}
	}
	if cfg.UserAgent == "" {
		cfg.UserAgent = "Mozilla/5.0 (compatible; agentic-ecommerce/3.5.0; AliExpressFallback)"
	}
	return &AliExpressClient{cfg: cfg, logger: logger}, nil
}

// Source returns the adapter's identifier.
func (c *AliExpressClient) Source() Source { return SourceAliExpress }

// Close marks the client closed.
func (c *AliExpressClient) Close(_ context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return nil
}

// Search returns ErrAliExpressStubMode in stub mode. The EC-7-2
// drop-ship agent never calls Search on the AliExpress fallback;
// the method exists to satisfy the Client interface.
func (c *AliExpressClient) Search(_ context.Context, req SearchRequest) ([]Product, error) {
	if strings.TrimSpace(req.Keyword) == "" {
		return nil, ErrInvalidQuery
	}
	c.mu.Lock()
	stub := c.cfg.StubMode || c.closed
	c.mu.Unlock()
	if stub {
		return nil, fmt.Errorf("%w: keyword=%q", ErrAliExpressStubMode, req.Keyword)
	}
	return nil, fmt.Errorf("china: aliexpress live search not implemented in v3.5.0 (set %s=1)", AliExpressStubEnvVar)
}

// ProductDetail returns ErrAliExpressStubMode in stub mode.
func (c *AliExpressClient) ProductDetail(_ context.Context, req ProductDetailRequest) (Product, error) {
	if req.ExternalID == "" {
		return Product{}, ErrInvalidQuery
	}
	c.mu.Lock()
	stub := c.cfg.StubMode || c.closed
	c.mu.Unlock()
	if stub {
		return Product{}, fmt.Errorf("%w: external_id=%q", ErrAliExpressStubMode, req.ExternalID)
	}
	return Product{}, fmt.Errorf("china: aliexpress live detail not implemented in v3.5.0 (set %s=1)", AliExpressStubEnvVar)
}

// StubMode reports whether the adapter is in stub-only mode.
func (c *AliExpressClient) StubMode() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cfg.StubMode
}
