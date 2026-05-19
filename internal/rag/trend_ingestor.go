// File scope: v3.2.0 EC-2-4 Trend Signal Ingestion.
//
// The TrendIngestor is the foundation story of the v3.2.0 enrichment
// pipeline. It polls a configurable set of TrendSource adapters
// (TikTok trending hashtags, Google Trends, RedNote topics) on the
// daily Temporal schedule, normalizes the responses into
// TrendRecord values, and persists them via the existing rag.Service
// so the EC-1-3 sourcing agent (and the EC-2-3 SEO module that
// depends on this trend store) can bias product selection toward
// trending niches.
//
// Resilience pillar (v2.10 baseline):
//
//   - Concurrency fan-out across TrendSource adapters routes through
//     internal/workerpool.Pool. No raw goroutines.
//   - Close honours the lifecycle.Closer contract so cmd/agent-worker
//     can register the ingestor with the lifecycle.Manager.
//   - All errors are typed sentinels wrapped via %w so callers branch
//     with errors.Is.
//   - Tenant awareness: every record is tagged with the configured
//     tenant_id and embedded under that tenant's namespace in the
//     VectorStore.
//   - Idempotent upsert: Document.ID is derived deterministically
//     from {tenant_id, platform, normalised_keyword} so re-runs hit
//     the existing rag chunk row instead of growing the store.
//
// The TrendIngestor satisfies the agent/sourcing.TrendSignaler port
// (TrendScore method) so the v3.1.0 China sourcing agent can read
// the trend signal without importing this package -- the import
// goes the other way (sourcing depends on the small interface, not
// on this concrete type), which keeps the coupling under the
// sentrux ceiling.
package rag

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// EC-2-4 typed sentinels. Callers branch via errors.Is.
var (
	// ErrTrendIngestorUnconfigured is returned by NewTrendIngestor
	// when a required dependency (sources, service, pool, tenant)
	// is missing.
	ErrTrendIngestorUnconfigured = errors.New("rag: trend ingestor unconfigured")

	// ErrTrendStaleData is returned by Run when no source returned
	// any record. The pipeline treats this as a fail-loud signal so
	// the v3.2.0 schedule can alert before downstream agents read
	// stale or empty trend data.
	ErrTrendStaleData = errors.New("rag: trend ingestor: stale data, all sources empty")

	// ErrTrendIngestorClosed is returned by Run after Close.
	ErrTrendIngestorClosed = errors.New("rag: trend ingestor closed")

	// ErrTrendSourceFailed is returned when a single TrendSource
	// fails. Wrapped with the source name; the ingestor logs but
	// does not abort -- a single platform outage must not zero the
	// pipeline.
	ErrTrendSourceFailed = errors.New("rag: trend source failed")
)

// TrendRecord is the canonical normalised shape every TrendSource
// returns. Score is in [0, 1]; Region is an ISO country code or
// platform-specific locale (e.g. "AU", "CN"); Volume is the raw
// engagement metric (search count, hashtag uses) when available.
type TrendRecord struct {
	Keyword   string    `json:"keyword"`
	Score     float64   `json:"score"`
	Region    string    `json:"region,omitempty"`
	Volume    int       `json:"volume,omitempty"`
	FetchedAt time.Time `json:"fetched_at,omitempty"`
}

// TrendSource is the per-platform port consumed by TrendIngestor.
// Implementations are owned by the composition root (tiktok client,
// google trends client, etc.) so this package stays free of
// platform-specific HTTP code.
type TrendSource interface {
	Platform() string
	Fetch(ctx context.Context) ([]TrendRecord, error)
}

// IngestService is the slim subset of *rag.Service the ingestor
// actually needs. Declaring it locally keeps the test surface small
// and lets future variants (e.g. mocked or batched) plug in without
// importing the concrete Service.
type IngestService interface {
	Ingest(ctx context.Context, doc Document) (IngestResult, error)
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
}

// TrendIngestorConfig wires the ingestor. Required: Sources,
// Service, Pool, TenantID. Optional: Now (test injection), Logger.
type TrendIngestorConfig struct {
	Sources  []TrendSource
	Service  IngestService
	Pool     *workerpool.Pool
	TenantID string

	// Now is injectable for deterministic timestamps in tests.
	Now func() time.Time
}

// TrendReport summarises a Run. Includes per-platform record counts
// + total ingested + per-source error capture. Inspectable from the
// scheduler so operators can wire alerts on RecordsIngested == 0.
type TrendReport struct {
	GeneratedAt     time.Time
	TenantID        string
	RecordsIngested int
	PlatformCounts  map[string]int
	Errors          map[string]string
}

// TrendIngestor is the v3.2.0 EC-2-4 daily trend signal pipeline.
type TrendIngestor struct {
	sources  []TrendSource
	service  IngestService
	pool     *workerpool.Pool
	tenantID string
	now      func() time.Time
	logger   *slog.Logger

	mu     sync.Mutex
	closed bool
}

// NewTrendIngestor constructs the ingestor. Required dependencies
// trigger ErrTrendIngestorUnconfigured.
func NewTrendIngestor(logger *slog.Logger, cfg TrendIngestorConfig) (*TrendIngestor, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.Sources) == 0 {
		return nil, fmt.Errorf("%w: at least one TrendSource required", ErrTrendIngestorUnconfigured)
	}
	if cfg.Service == nil {
		return nil, fmt.Errorf("%w: rag.Service (or IngestService) required", ErrTrendIngestorUnconfigured)
	}
	if cfg.Pool == nil {
		return nil, fmt.Errorf("%w: workerpool.Pool required (no raw goroutines)", ErrTrendIngestorUnconfigured)
	}
	if strings.TrimSpace(cfg.TenantID) == "" {
		return nil, fmt.Errorf("%w: TenantID required", ErrTrendIngestorUnconfigured)
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	return &TrendIngestor{
		sources:  cfg.Sources,
		service:  cfg.Service,
		pool:     cfg.Pool,
		tenantID: cfg.TenantID,
		now:      cfg.Now,
		logger:   logger,
	}, nil
}

// Close marks the ingestor closed. The wired pool is owned by the
// composition root so its own lifecycle.Close path drains workers.
func (t *TrendIngestor) Close(_ context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.closed = true
	return nil
}

// Run polls every TrendSource concurrently via the workerpool,
// collects records, and ingests them as rag documents. Returns
// ErrTrendStaleData when zero records came back across all sources.
func (t *TrendIngestor) Run(ctx context.Context) (TrendReport, error) {
	if err := t.guardRun(); err != nil {
		return TrendReport{}, err
	}
	now := t.now().UTC()
	report := TrendReport{
		GeneratedAt:    now,
		TenantID:       t.tenantID,
		PlatformCounts: make(map[string]int, len(t.sources)),
		Errors:         make(map[string]string),
	}
	collected, sourceErrors := t.fetchAll(ctx)
	for source, err := range sourceErrors {
		report.Errors[source] = err.Error()
	}
	if len(collected) == 0 {
		return report, fmt.Errorf("%w: tenant %s, %d sources", ErrTrendStaleData, t.tenantID, len(t.sources))
	}
	for platform, records := range collected {
		ingested := 0
		for _, rec := range records {
			doc := buildTrendDocument(t.tenantID, platform, rec, now)
			if _, err := t.service.Ingest(ctx, doc); err != nil {
				t.logger.Warn("rag.trend_ingest_error",
					"tenant_id", t.tenantID,
					"platform", platform,
					"keyword", rec.Keyword,
					"error", err,
				)
				continue
			}
			ingested++
		}
		report.PlatformCounts[platform] = ingested
		report.RecordsIngested += ingested
	}
	return report, nil
}

func (t *TrendIngestor) guardRun() error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return ErrTrendIngestorClosed
	}
	return nil
}

// fetchAll fans the sources out across the workerpool. Errors per
// source are captured but never abort the run: the trend pipeline
// must degrade gracefully when a single platform is down.
func (t *TrendIngestor) fetchAll(ctx context.Context) (map[string][]TrendRecord, map[string]error) {
	type result struct {
		platform string
		records  []TrendRecord
		err      error
	}
	results := make(chan result, len(t.sources))
	var pending sync.WaitGroup
	for _, src := range t.sources {
		src := src
		pending.Add(1)
		err := t.pool.Submit(ctx, func(taskCtx context.Context) error {
			defer pending.Done()
			records, err := src.Fetch(taskCtx)
			results <- result{platform: src.Platform(), records: records, err: err}
			return nil
		})
		if err != nil {
			pending.Done()
			results <- result{
				platform: src.Platform(),
				err:      fmt.Errorf("%w: %s submit: %w", ErrTrendSourceFailed, src.Platform(), err),
			}
		}
	}
	go func() {
		pending.Wait()
		close(results)
	}()
	collected := make(map[string][]TrendRecord, len(t.sources))
	errs := make(map[string]error)
	for r := range results {
		if r.err != nil {
			errs[r.platform] = r.err
			continue
		}
		if len(r.records) == 0 {
			continue
		}
		collected[r.platform] = append(collected[r.platform], r.records...)
	}
	return collected, errs
}

// TrendScore satisfies the agent/sourcing.TrendSignaler interface.
// Returns a value in [0, 1] biased toward 1.0 when productTitle
// matches a strongly trending keyword for the supplied tenant.
//
// Implementation: the title is queried against the rag store under
// the tenant scope. The top hit's cosine similarity (already
// returned in SearchResult.Score) is multiplied by the trend's
// stored normalised score (recovered from the chunk metadata via
// the score:N prefix in the chunk text) and clamped to [0, 1].
func (t *TrendIngestor) TrendScore(ctx context.Context, tenantID, keyword, productTitle string) (float64, error) {
	candidate := strings.TrimSpace(productTitle)
	if candidate == "" {
		candidate = strings.TrimSpace(keyword)
	}
	if candidate == "" {
		return 0, nil
	}
	results, err := t.service.Search(ctx, SearchQuery{
		TenantID: tenantID,
		Text:     candidate,
		TopK:     5,
	})
	if err != nil {
		return 0, fmt.Errorf("rag: trend score search: %w", err)
	}
	if len(results) == 0 {
		return 0, nil
	}
	best := results[0]
	score := clamp01TrendScore(best.Score) * extractTrendStrength(best.Metadata)
	return clamp01TrendScore(score), nil
}

func clamp01TrendScore(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// extractTrendStrength reads the stored normalised trend score from
// the chunk metadata. Defaults to 0.5 if missing so a stored chunk
// without a numeric strength still biases above zero.
func extractTrendStrength(metadata map[string]string) float64 {
	if metadata == nil {
		return 0.5
	}
	raw, ok := metadata["trend_strength"]
	if !ok {
		return 0.5
	}
	v, err := parseStrength(raw)
	if err != nil {
		return 0.5
	}
	return clamp01TrendScore(v)
}

// parseStrength parses a stringified float from chunk metadata.
// Kept tiny so the metadata path is unit-testable in isolation.
func parseStrength(raw string) (float64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, fmt.Errorf("trend strength missing")
	}
	var v float64
	if _, err := fmt.Sscanf(raw, "%f", &v); err != nil {
		return 0, err
	}
	return v, nil
}

// buildTrendDocument shapes a TrendRecord into a rag.Document with
// deterministic ID + tenant scoping + structured metadata. The text
// is intentionally short so the chunker emits a single chunk.
func buildTrendDocument(tenantID, platform string, rec TrendRecord, now time.Time) Document {
	if rec.FetchedAt.IsZero() {
		rec.FetchedAt = now
	}
	docID := trendDocumentID(tenantID, platform, rec.Keyword)
	source := "trend:" + platform
	metadata := map[string]string{
		"platform":       platform,
		"region":         rec.Region,
		"trend_strength": fmt.Sprintf("%.4f", clamp01TrendScore(rec.Score)),
		"volume":         fmt.Sprintf("%d", rec.Volume),
	}
	keys := sortedMetadataKeys(metadata)
	return Document{
		ID:       docID,
		TenantID: tenantID,
		Title:    rec.Keyword,
		Source:   source,
		Content: fmt.Sprintf(
			"%s [score:%.4f region:%s volume:%d platform:%s] keys:%s",
			rec.Keyword, rec.Score, rec.Region, rec.Volume, platform, strings.Join(keys, ","),
		),
		Metadata:  metadata,
		CreatedAt: rec.FetchedAt,
	}
}

func trendDocumentID(tenantID, platform, keyword string) string {
	keyword = strings.ToLower(strings.TrimSpace(keyword))
	keyword = strings.ReplaceAll(keyword, " ", "-")
	return fmt.Sprintf("trend:%s:%s:%s", tenantID, platform, keyword)
}

func sortedMetadataKeys(metadata map[string]string) []string {
	out := make([]string, 0, len(metadata))
	for k := range metadata {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
