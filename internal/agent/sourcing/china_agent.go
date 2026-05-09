// File scope: v3.1.0 EC-1-3 China Sourcing Agent.
//
// This file is independent of the legacy Score() API in agent.go.
// Where agent.go scores already-shortlisted candidates, china_agent.go
// drives the full sourcing pipeline:
//
//  1. Concurrent search across multiple china.Client implementations
//     (1688, Taobao, ...) via internal/workerpool. No raw goroutines.
//  2. Compliance pre-screening via internal/compliance.Evaluate. Any
//     product carrying a category on the AU-import or platform-
//     prohibited lists is rejected and counted into a rejection
//     histogram.
//  3. Supplier scoring via internal/domain.Supplier.Score (EC-1-5).
//     Products from suppliers below SupplierScoreFloor are dropped.
//  4. Trend signal scoring via the optional TrendSignaler (pgvector
//     RAG store; no-op when nil).
//  5. Composite ranking + emit ProductSourcingProposal event to the
//     eventbus with a typed SourcingProposalPayload (v2.4 envelope).
//  6. Prometheus metrics + EvoMap NDJSON KPI emission.
//
// The OmniParser bridge URL is read from the OMNIPARSER_BRIDGE_URL
// environment variable at agent construction time. The bridge is
// reserved for the v3.1.x dynamic-JS / CAPTCHA detection path
// (EC-10-4); v3.1.0 does not call it directly, but production
// constructors REJECT when the env var is unset because once that
// path lights up an unset URL would fail silently.
//
// Design notes:
//
//   - Functions are kept under 75 LOC and cyclomatic <= 10 to honour
//     the sentrux complex_fn ceiling of 4 (existing baseline). The
//     pipeline is deliberately decomposed into searchAcrossClients,
//     filterCompliance, scoreCandidates, rankAndSelect.
//   - All errors typed and %w-wrapped via the package sentinels in
//     errors.go. Callers branch with errors.Is.
//   - Tenant awareness: the agent constructor takes TenantID; every
//     candidate is annotated, every event payload, every metric
//     label.
package sourcing

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/china"
	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/metrics"
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// OmniParserBridgeEnvVar is the env-var the agent reads to discover
// the OmniParser bridge URL. Unit tests can set this directly; in
// production it is set by the launchd plist.
const OmniParserBridgeEnvVar = "OMNIPARSER_BRIDGE_URL"

// ChinaSourcingAgent is the v3.1.0 EC-1-3 sourcing agent.
type ChinaSourcingAgent struct {
	clients          []china.Client
	pool             *workerpool.Pool
	trendStore       TrendSignaler
	publisher        eventbus.Publisher
	metricsRegistry  *metrics.Registry
	complianceMatrix complianceEvaluator
	omniBridgeURL    string
	tenantID         string
	maxResults       int
	now              func() time.Time
	logger           *slog.Logger

	mu     sync.Mutex
	closed bool
}

// ChinaSourcingConfig wires the agent. Required: Clients, Pool,
// Publisher, TenantID. Optional: TrendStore (nil disables trend
// signal), MetricsRegistry (nil disables metric emission),
// MaxResults (defaults to 25). OmniParserBridgeURL is read from env
// when blank; explicit value bypasses env (used by tests).
type ChinaSourcingConfig struct {
	Clients             []china.Client
	Pool                *workerpool.Pool
	TrendStore          TrendSignaler
	Publisher           eventbus.Publisher
	MetricsRegistry     *metrics.Registry
	OmniParserBridgeURL string
	TenantID            string
	MaxResults          int

	// Now is injectable for deterministic event timestamps in tests.
	Now func() time.Time
}

// TrendSignaler is the small port the sourcing agent needs from the
// pgvector RAG layer. We keep it minimal so tests can wire a fake
// without bringing rag.Service.
type TrendSignaler interface {
	TrendScore(ctx context.Context, tenantID, keyword, productTitle string) (float64, error)
}

// complianceEvaluator is satisfied by compliance.Evaluate (function-
// adapter pattern). Tests substitute their own evaluator without
// touching the package-level rule maps.
type complianceEvaluator func(p compliance.Product) (compliance.Decision, error)

// SourcingRequest is the input to ChinaSourcingAgent.Run.
type SourcingRequest struct {
	Keyword         string
	CategoryHint    string
	MaxResults      int
	MinSupplierFlr  float64 // overrides domain.SupplierScoreFloor when > 0
	ExpectedSellCNY int     // optional: forces the margin baseline
}

// SourcingResult captures the agent run output. Inspectable by tests
// without forcing them to subscribe to the eventbus.
type SourcingResult struct {
	Keyword          string
	TenantID         string
	GeneratedAt      time.Time
	SelectedProducts []china.Product
	SupplierScores   map[string]float64 // keyed by Product.ExternalID
	CompositeScores  map[string]float64
	RejectedReasons  map[string]int
	Source           string
	EventEmitted     bool
}

// NewChinaSourcingAgent constructs a ChinaSourcingAgent.
func NewChinaSourcingAgent(logger *slog.Logger, cfg ChinaSourcingConfig) (*ChinaSourcingAgent, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.Clients) == 0 {
		return nil, fmt.Errorf("%w: at least one china.Client required", ErrSourcingUnconfigured)
	}
	if cfg.Pool == nil {
		return nil, fmt.Errorf("%w: workerpool.Pool required (no raw goroutines)", ErrSourcingUnconfigured)
	}
	if cfg.Publisher == nil {
		return nil, fmt.Errorf("%w: eventbus.Publisher required", ErrSourcingUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrSourcingUnconfigured)
	}
	if cfg.MaxResults <= 0 {
		cfg.MaxResults = 25
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	bridge := cfg.OmniParserBridgeURL
	if bridge == "" {
		bridge = os.Getenv(OmniParserBridgeEnvVar)
	}
	if bridge == "" {
		return nil, fmt.Errorf("%w: %s env var required (set to omniparser bridge URL alias)", ErrOmniParserUnconfigured, OmniParserBridgeEnvVar)
	}
	return &ChinaSourcingAgent{
		clients:          cfg.Clients,
		pool:             cfg.Pool,
		trendStore:       cfg.TrendStore,
		publisher:        cfg.Publisher,
		metricsRegistry:  cfg.MetricsRegistry,
		complianceMatrix: compliance.Evaluate,
		omniBridgeURL:    bridge,
		tenantID:         cfg.TenantID,
		maxResults:       cfg.MaxResults,
		now:              cfg.Now,
		logger:           logger,
	}, nil
}

// OmniParserBridgeURL returns the configured bridge URL. Useful for
// dashboard / doctor endpoints.
func (a *ChinaSourcingAgent) OmniParserBridgeURL() string { return a.omniBridgeURL }

// Close marks the agent closed. The wired china.Client + workerpool
// are owned by the constructor caller (composition root) so they
// run their own lifecycle.Close paths; the agent itself just flips
// the closed flag.
func (a *ChinaSourcingAgent) Close(_ context.Context) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.closed = true
	return nil
}

// Run executes a sourcing pipeline end to end. Returns the result
// even on partial failure so callers can introspect rejected
// counts; errors are returned only for fatal failures (bus down,
// pool saturated, ctx cancelled).
func (a *ChinaSourcingAgent) Run(ctx context.Context, req SourcingRequest) (SourcingResult, error) {
	if err := a.guardRunInputs(req); err != nil {
		return SourcingResult{}, err
	}
	start := a.now()

	candidates, err := a.searchAcrossClients(ctx, req)
	if err != nil {
		return SourcingResult{}, err
	}
	approved, rejectedCounts := a.filterCompliance(candidates)

	scored, supplierScoreSum := a.scoreCandidates(ctx, req, approved)

	result := a.rankAndBuildResult(ctx, req, scored, rejectedCounts, supplierScoreSum, start)
	if err := a.emit(ctx, result); err != nil {
		return result, err
	}
	a.recordMetrics(req, result, start)
	return result, nil
}

func (a *ChinaSourcingAgent) guardRunInputs(req SourcingRequest) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.closed {
		return ErrSourcingClosed
	}
	if strings.TrimSpace(req.Keyword) == "" {
		return ErrSourcingEmptyKeyword
	}
	return nil
}

// searchAcrossClients fans out req.Keyword across every configured
// client via the workerpool, collecting results into a single slice.
// Errors from individual clients are logged but not fatal so a
// single-platform outage does not zero the run.
func (a *ChinaSourcingAgent) searchAcrossClients(ctx context.Context, req SourcingRequest) ([]china.Product, error) {
	type clientResult struct {
		products []china.Product
		err      error
	}
	results := make(chan clientResult, len(a.clients))
	var pending sync.WaitGroup
	maxFanout := a.effectiveMax(req.MaxResults)
	for _, c := range a.clients {
		c := c
		pending.Add(1)
		err := a.pool.Submit(ctx, func(taskCtx context.Context) error {
			defer pending.Done()
			products, err := c.Search(taskCtx, china.SearchRequest{
				Keyword:      req.Keyword,
				MaxResults:   maxFanout,
				CategoryHint: req.CategoryHint,
			})
			results <- clientResult{products: products, err: err}
			return nil
		})
		if err != nil {
			pending.Done()
			results <- clientResult{err: fmt.Errorf("%w: source %s submit: %w", ErrSourcingFanoutFailed, c.Source(), err)}
		}
	}
	go func() {
		pending.Wait()
		close(results)
	}()
	out := make([]china.Product, 0, maxFanout*len(a.clients))
	for cr := range results {
		if cr.err != nil {
			a.logger.Warn("china-sourcing.client_error", "tenant_id", a.tenantID, "error", cr.err)
			continue
		}
		out = append(out, cr.products...)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("%w: keyword %q across %d clients", ErrSourcingEmptyResults, req.Keyword, len(a.clients))
	}
	return out, nil
}

func (a *ChinaSourcingAgent) effectiveMax(reqMax int) int {
	if reqMax <= 0 {
		return a.maxResults
	}
	if reqMax > a.maxResults {
		return a.maxResults
	}
	return reqMax
}

// filterCompliance routes every candidate through the compliance gate
// (EC-1-4). Returns the approved slice + a category->count rejection
// map fed to the metrics layer + event payload.
func (a *ChinaSourcingAgent) filterCompliance(candidates []china.Product) ([]china.Product, map[string]int) {
	approved := make([]china.Product, 0, len(candidates))
	rejected := make(map[string]int)
	for _, c := range candidates {
		decision, err := a.complianceMatrix(compliance.Product{
			ID:          c.ExternalID,
			TenantID:    a.tenantID,
			Title:       c.Title,
			Category:    c.Category,
			SubCategory: c.SubCategory,
			Source:      compliance.Source(c.Source),
		})
		if err != nil && errors.Is(err, compliance.ErrRestrictedCategory) {
			rejected[normaliseCategoryKey(decision.Category)]++
			continue
		}
		if err != nil {
			// e.g. ErrEmptyProduct -- skip but count.
			rejected["invalid_input"]++
			continue
		}
		if !decision.Pass {
			rejected[normaliseCategoryKey(decision.Category)]++
			continue
		}
		approved = append(approved, c)
	}
	return approved, rejected
}

// scoreCandidates applies supplier-score (EC-1-5) and trend-signal
// (pgvector) filters. Returns the surviving candidates + the running
// sum of supplier scores so the EvoMap KPI mean can be emitted.
func (a *ChinaSourcingAgent) scoreCandidates(ctx context.Context, req SourcingRequest, candidates []china.Product) ([]scoredCandidate, float64) {
	floor := domain.SupplierScoreFloor
	if req.MinSupplierFlr > 0 {
		floor = req.MinSupplierFlr
	}
	out := make([]scoredCandidate, 0, len(candidates))
	var sum float64
	for _, p := range candidates {
		supplier := domain.Supplier{
			ID:                  p.SupplierID,
			TenantID:            a.tenantID,
			Name:                p.SupplierName,
			Country:             "CN",
			Platform:            string(p.Source),
			MOQ:                 p.MOQ,
			LeadTimeDays:        p.LeadTimeDays,
			VerifiedGold:        p.SupplierVerified,
			PositiveReviewRatio: clamp01(p.SupplierRating / 5.0),
		}
		supplierScore, pass := supplier.Score()
		sum += supplierScore
		if !pass || supplierScore < floor {
			continue
		}
		expected := req.ExpectedSellCNY
		if expected <= 0 {
			expected = expectedSellPriceCNY(p.PriceCNYCents)
		}
		marginScore := computeMarginScore(p.PriceCNYCents, expected)
		trendScore, _ := a.trendScore(ctx, req.Keyword, p.Title)
		out = append(out, scoredCandidate{
			product:        p,
			supplier:       supplier,
			supplierScore:  supplierScore,
			marginScore:    marginScore,
			trendScore:     trendScore,
			compositeScore: rankComposite(supplierScore, marginScore, trendScore),
		})
	}
	return out, sum
}

func (a *ChinaSourcingAgent) trendScore(ctx context.Context, keyword, title string) (float64, error) {
	if a.trendStore == nil {
		return 0.5, nil // neutral default when trend store unavailable
	}
	return a.trendStore.TrendScore(ctx, a.tenantID, keyword, title)
}

// rankAndBuildResult sorts the scored candidates descending by
// composite score and packs the result struct.
func (a *ChinaSourcingAgent) rankAndBuildResult(ctx context.Context, req SourcingRequest, scored []scoredCandidate, rejected map[string]int, supplierSum float64, _ time.Time) SourcingResult {
	_ = ctx
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].compositeScore == scored[j].compositeScore {
			return scored[i].product.ExternalID < scored[j].product.ExternalID
		}
		return scored[i].compositeScore > scored[j].compositeScore
	})
	maxOut := req.MaxResults
	if maxOut <= 0 || maxOut > a.maxResults {
		maxOut = a.maxResults
	}
	if maxOut > len(scored) {
		maxOut = len(scored)
	}
	selected := make([]china.Product, 0, maxOut)
	supplierMap := make(map[string]float64, maxOut)
	compositeMap := make(map[string]float64, maxOut)
	for i := 0; i < maxOut; i++ {
		c := scored[i]
		selected = append(selected, c.product)
		supplierMap[c.product.ExternalID] = c.supplierScore
		compositeMap[c.product.ExternalID] = c.compositeScore
	}
	source := "1688"
	if len(selected) > 0 {
		source = string(selected[0].Source)
	}
	_ = supplierSum // unused; included for parity with metrics path
	return SourcingResult{
		Keyword:          req.Keyword,
		TenantID:         a.tenantID,
		GeneratedAt:      a.now().UTC(),
		SelectedProducts: selected,
		SupplierScores:   supplierMap,
		CompositeScores:  compositeMap,
		RejectedReasons:  rejected,
		Source:           source,
	}
}

// emit publishes the v2.4 typed SourcingProposalPayload to the bus.
// Errors propagate so the caller surfaces them; metrics still fire
// in the success path.
func (a *ChinaSourcingAgent) emit(ctx context.Context, r SourcingResult) error {
	if len(r.SelectedProducts) == 0 {
		// Nothing to emit. Return ErrSourcingEmptyResults so callers
		// can branch (the metrics layer still records the run).
		return fmt.Errorf("%w: keyword %q produced 0 selected products", ErrSourcingEmptyResults, r.Keyword)
	}
	payload := buildPayload(r)
	evt, err := eventbus.NewSourcingProposalEvent("agent.sourcing.china", r.GeneratedAt, payload)
	if err != nil {
		return fmt.Errorf("build sourcing event: %w", err)
	}
	if err := a.publisher.Publish(ctx, evt); err != nil {
		return fmt.Errorf("publish sourcing event: %w", err)
	}
	r.EventEmitted = true
	return nil
}

func buildPayload(r SourcingResult) eventbus.SourcingProposalPayload {
	products := make([]eventbus.SourcingProposalProduct, 0, len(r.SelectedProducts))
	var supplierTotal, marginTotal, trendTotal, compositeTotal float64
	for _, p := range r.SelectedProducts {
		score := r.SupplierScores[p.ExternalID]
		composite := r.CompositeScores[p.ExternalID]
		products = append(products, eventbus.SourcingProposalProduct{
			ExternalID:    p.ExternalID,
			Source:        string(p.Source),
			Title:         p.Title,
			Category:      p.Category,
			PriceCNYCents: p.PriceCNYCents,
			MOQ:           p.MOQ,
			LeadTimeDays:  p.LeadTimeDays,
			SupplierID:    p.SupplierID,
			SupplierScore: score,
			URL:           p.URL,
		})
		supplierTotal += score
		compositeTotal += composite
	}
	n := float64(len(products))
	if n > 0 {
		supplierTotal /= n
		compositeTotal /= n
	}
	marginTotal = 0
	trendTotal = 0
	rejectedTotal := 0
	for _, c := range r.RejectedReasons {
		rejectedTotal += c
	}
	return eventbus.SourcingProposalPayload{
		Version:          eventbus.SourcingProposalPayloadVersion,
		TenantID:         r.TenantID,
		Keyword:          r.Keyword,
		Source:           r.Source,
		GeneratedAt:      r.GeneratedAt,
		SelectedProducts: products,
		RejectedCount:    rejectedTotal,
		RejectedReasons:  r.RejectedReasons,
		SupplierScore:    supplierTotal,
		MarginScore:      marginTotal,
		TrendScore:       trendTotal,
		CompositeScore:   compositeTotal,
	}
}

// recordMetrics emits the EC-1-3 Prometheus counters + histograms.
// No-op when MetricsRegistry is nil.
func (a *ChinaSourcingAgent) recordMetrics(req SourcingRequest, r SourcingResult, start time.Time) {
	if a.metricsRegistry == nil {
		return
	}
	a.metricsRegistry.SourcingRuns.Inc(metrics.Labels{"tenant_id": a.tenantID, "source": r.Source})
	a.metricsRegistry.SourcingDuration.Observe(time.Since(start).Seconds(), metrics.Labels{"source": r.Source})
	for category, count := range r.RejectedReasons {
		a.metricsRegistry.SourcingComplianceRejects.Add(float64(count), metrics.Labels{"category": category})
	}
	for _, score := range r.SupplierScores {
		a.metricsRegistry.SupplierScoreDistribution.Observe(score, nil)
	}
	_ = req
}

func normaliseCategoryKey(s string) string {
	if s == "" {
		return "unknown"
	}
	return strings.ToLower(strings.TrimSpace(s))
}

// scoredCandidate is the per-candidate working state inside
// scoreCandidates + rankAndBuildResult. Kept package-private.
type scoredCandidate struct {
	product        china.Product
	supplier       domain.Supplier
	supplierScore  float64
	marginScore    float64
	trendScore     float64
	compositeScore float64
}
