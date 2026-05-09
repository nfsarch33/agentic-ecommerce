// File scope: v3.2.0 EC-2-3 SEO keyword injection + WooCommerce
// catalogue sync.
//
// The ProductSEO injector is the third leg of the enrichment
// pipeline (after EC-2-1 description gen + EC-2-2 hero image). It
// pulls trending long-tail keywords from the EC-2-4 trend store
// (consumed via the TrendKeywordSource port -- the cmd/* binary
// wires a small adapter on top of rag.TrendIngestor.TrendScore so
// this package does NOT import internal/rag and the coupling
// surface stays flat) and feeds them into the existing Optimizer
// to produce a Suggestion. The Suggestion is then handed to the
// CatalogueImporter port for an idempotent WooCommerce upsert.
//
// Cross-repo touchpoint: the production WooCommerce idempotent
// importer per the external roadmap may live in
// `ai-agent-business-stack`. To keep this sprint scoped, the
// CatalogueImporter port is defined here and the EC-side
// composition root (cmd/agent-worker) wires either the
// internal/adapter/woocommerce.Client (already in this repo) or a
// future business-stack adapter -- both satisfy the small port.
//
// Resilience pillar (v2.10 baseline):
//
//   - Implements lifecycle.Closer.
//   - Synchronous Inject -- no raw goroutines. Batch fan-out
//     (50-product enrichment job) submits via internal/workerpool
//     in cmd/agent-worker.
//   - All errors typed + %w-wrapped via package sentinels.
//   - Tenant awareness: every Inject call scoped by the configured
//     TenantID; the WC importer Upsert payload also carries it.
//   - Idempotency: SKU + tenant_id is the natural composite key.
//     The CatalogueImporter implementation MUST upsert on this
//     key; the test fakeImporter mirrors that contract.
//
// Cite skill: go-clean-architecture (port + adapter; the importer
// + trend source ports keep this package free of WooCommerce HTTP
// detail and rag pgvector detail).
package seo

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"
)

// EC-2-3 typed sentinels.
var (
	ErrSEOUnconfigured = errors.New("seo: product injector unconfigured")
	ErrSEOClosed       = errors.New("seo: product injector closed")
	ErrSEONoTrendData  = errors.New("seo: no trend data available")
)

// SEOProduct is the v3.2.0 input subset.
type SEOProduct struct {
	ID          string
	Title       string
	Description string
	Topic       string // e.g. "earbuds" -- the trend store lookup key
	Categories  []string
	PriceCents  int
	Stock       int
}

// SEOInjectRequest is the unit of work submitted to Inject.
type SEOInjectRequest struct {
	Product           SEOProduct
	AdditionalKeyword string // optional manual keyword from the operator
}

// SEOInjectResult carries the optimised Suggestion + the WC sync
// outcome.
type SEOInjectResult struct {
	ProductID     string
	TenantID      string
	Suggestion    Suggestion
	UsedTrendData bool
	Sync          CatalogueUpsertResult
	GeneratedAt   time.Time
}

// TrendKeywordSource is the slim port the SEO injector needs from
// the EC-2-4 trend store. The composition root wires an adapter
// that calls rag.TrendIngestor.TrendScore() to discover the top
// trending keywords for the topic.
type TrendKeywordSource interface {
	TrendingKeywords(ctx context.Context, tenantID, topic string) ([]string, error)
}

// CatalogueUpsertRequest is the platform-agnostic payload handed
// to the WooCommerce idempotent importer (or its business-stack
// equivalent).
type CatalogueUpsertRequest struct {
	TenantID    string
	SKU         string
	Title       string
	Description string
	MetaTitle   string
	MetaDesc    string
	Tags        []string
	Categories  []string
	PriceCents  int
	Stock       int
}

// CatalogueUpsertResult captures whether the upsert created a new
// product or no-op'd on an existing SKU.
type CatalogueUpsertResult struct {
	SKU     string
	Created bool
}

// CatalogueImporter is the port the SEO injector calls to push
// the enriched product into WooCommerce. The cmd/* binary wires
// either internal/adapter/woocommerce.Client (this repo) or a
// future business-stack adapter; both must be idempotent on
// (tenant_id, sku).
type CatalogueImporter interface {
	Upsert(ctx context.Context, req CatalogueUpsertRequest) (CatalogueUpsertResult, error)
}

// ProductSEOConfig wires the injector.
type ProductSEOConfig struct {
	Trends       TrendKeywordSource
	Importer     CatalogueImporter
	TenantID     string
	StrictTrends bool             // if true, trend store error => fail Inject
	Now          func() time.Time // injectable clock for tests
}

// ProductSEO is the v3.2.0 EC-2-3 agent.
type ProductSEO struct {
	trends       TrendKeywordSource
	importer     CatalogueImporter
	tenantID     string
	strictTrends bool
	now          func() time.Time
	optimizer    Optimizer
	logger       *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewProductSEO constructs the injector.
func NewProductSEO(logger *slog.Logger, cfg ProductSEOConfig) (*ProductSEO, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.Trends == nil {
		return nil, fmt.Errorf("%w: TrendKeywordSource required", ErrSEOUnconfigured)
	}
	if cfg.Importer == nil {
		return nil, fmt.Errorf("%w: CatalogueImporter required", ErrSEOUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrSEOUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &ProductSEO{
		trends:       cfg.Trends,
		importer:     cfg.Importer,
		tenantID:     cfg.TenantID,
		strictTrends: cfg.StrictTrends,
		now:          cfg.Now,
		optimizer:    NewOptimizer(),
		logger:       logger,
	}, nil
}

// Close marks the injector closed. lifecycle.Closer contract.
func (p *ProductSEO) Close(_ context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	return nil
}

// Inject pulls trending keywords, blends them into the optimiser
// inputs, and calls the WooCommerce importer.
func (p *ProductSEO) Inject(ctx context.Context, req SEOInjectRequest) (SEOInjectResult, error) {
	if err := p.guardInject(req); err != nil {
		return SEOInjectResult{}, err
	}
	keywords, used, err := p.fetchKeywords(ctx, req)
	if err != nil {
		return SEOInjectResult{}, err
	}

	suggestion := p.optimizer.Suggest(Input{
		Title:       p.composeTitle(req.Product.Title, keywords),
		Description: req.Product.Description,
		Keywords:    keywords,
	})
	suggestion = p.injectKeywordsIntoMeta(suggestion, keywords)

	sync, err := p.importer.Upsert(ctx, CatalogueUpsertRequest{
		TenantID:    p.tenantID,
		SKU:         req.Product.ID,
		Title:       suggestion.Title,
		Description: req.Product.Description,
		MetaTitle:   suggestion.Title,
		MetaDesc:    suggestion.MetaDescription,
		Tags:        keywords,
		Categories:  req.Product.Categories,
		PriceCents:  req.Product.PriceCents,
		Stock:       req.Product.Stock,
	})
	if err != nil {
		return SEOInjectResult{}, fmt.Errorf("seo: catalogue upsert: %w", err)
	}
	return SEOInjectResult{
		ProductID:     req.Product.ID,
		TenantID:      p.tenantID,
		Suggestion:    suggestion,
		UsedTrendData: used,
		Sync:          sync,
		GeneratedAt:   p.now().UTC(),
	}, nil
}

func (p *ProductSEO) guardInject(req SEOInjectRequest) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrSEOClosed
	}
	if strings.TrimSpace(req.Product.ID) == "" {
		return fmt.Errorf("%w: Product.ID required", ErrSEOUnconfigured)
	}
	return nil
}

// fetchKeywords routes through the trend store. Returns (keywords,
// usedTrendData, err). In non-strict mode the upstream error is
// logged + a fallback (empty keyword list) is returned so the
// pipeline degrades gracefully. In strict mode the error is
// surfaced as ErrSEONoTrendData wrapping the upstream cause.
func (p *ProductSEO) fetchKeywords(ctx context.Context, req SEOInjectRequest) ([]string, bool, error) {
	keywords, err := p.trends.TrendingKeywords(ctx, p.tenantID, req.Product.Topic)
	if err != nil {
		if p.strictTrends {
			return nil, false, fmt.Errorf("%w: %w", ErrSEONoTrendData, err)
		}
		p.logger.Warn("seo.trend_lookup_error",
			"tenant_id", p.tenantID,
			"topic", req.Product.Topic,
			"error", err,
		)
		return p.fallbackKeywords(req), false, nil
	}
	keywords = sanitiseKeywords(keywords)
	if req.AdditionalKeyword != "" {
		keywords = append(keywords, strings.TrimSpace(req.AdditionalKeyword))
	}
	if len(keywords) == 0 {
		return p.fallbackKeywords(req), false, nil
	}
	return keywords, true, nil
}

func (p *ProductSEO) fallbackKeywords(req SEOInjectRequest) []string {
	parts := []string{}
	if t := strings.TrimSpace(req.Product.Topic); t != "" {
		parts = append(parts, t)
	}
	if t := strings.TrimSpace(req.Product.Title); t != "" {
		parts = append(parts, strings.ToLower(t))
	}
	return sanitiseKeywords(parts)
}

// composeTitle blends the product title with the top-1 trending
// keyword, capped at the existing 60-rune SEO ceiling. Conservative
// composition keeps the existing seo.Optimizer keyword-density
// scoring intact.
func (p *ProductSEO) composeTitle(title string, keywords []string) string {
	if len(keywords) == 0 {
		return title
	}
	top := keywords[0]
	if strings.Contains(strings.ToLower(title), strings.ToLower(top)) {
		return title
	}
	candidate := top + " | " + title
	if runeLen(candidate) > maxTitleRunes {
		return title
	}
	return candidate
}

// injectKeywordsIntoMeta appends the top-2 trending keywords to
// the meta description if not already present. Uses sentence-cap
// to stay under the 155-char ceiling.
func (p *ProductSEO) injectKeywordsIntoMeta(s Suggestion, keywords []string) Suggestion {
	if len(keywords) == 0 {
		return s
	}
	cap := 2
	if cap > len(keywords) {
		cap = len(keywords)
	}
	tail := strings.Join(keywords[:cap], ", ")
	current := strings.TrimSpace(s.MetaDescription)
	addition := tail + "."
	if current != "" && !strings.HasSuffix(current, ".") && !strings.HasSuffix(current, "!") {
		addition = ". " + addition
	} else if current != "" {
		addition = " " + addition
	}
	candidate := current + addition
	if runeLen(candidate) > maxMetaRunes {
		s.MetaDescription = truncateSentence(candidate, maxMetaRunes)
	} else {
		s.MetaDescription = candidate
	}
	return s
}

func sanitiseKeywords(in []string) []string {
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, k := range in {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		out = append(out, k)
	}
	sort.SliceStable(out, func(i, j int) bool {
		// Preserve "trending order" by length (longer long-tails
		// first). Stable sort keeps original order for ties.
		return len(out[i]) > len(out[j])
	})
	return out
}
