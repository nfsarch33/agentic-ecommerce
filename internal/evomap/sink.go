// Package evomap implements the v2.10.0 Story 5 NDJSON sink that
// feeds the EvoMap-Evolver / EvoLoop-DRL pipeline. Each EC binary
// writes one Capsule per minute (driven by memwatch.Sampler) into a
// rotating NDJSON file at tests/metrics/evomap.ndjson; the daily
// rollup binary (cmd/evomap-rollup) aggregates them into a markdown
// capsule consumed by the existing fleet evoloop pipeline.
//
// Design notes:
//   - Append-only with one JSON object per line (NDJSON contract).
//   - Daily rotation by ISO date suffix when Rotate=true.
//   - Reopens existing files for append on restart so cmd/* restarts
//     do not lose history.
//   - Schema mirrors the existing fleet evoloop capsule; this keeps
//     selfimprove.RecordCycle a drop-in consumer.
package evomap

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Capsule is the single-line NDJSON record emitted to evomap.ndjson.
// Field tags use snake_case to match the existing fleet capsule
// schema in evoloop-capsules/.
type Capsule struct {
	RecordedAt time.Time `json:"recorded_at"`
	EventAt    time.Time `json:"event_at"`
	Binary     string    `json:"binary"`
	TenantID   string    `json:"tenant_id,omitempty"`
	KPIs       KPIs      `json:"kpis"`
}

// KPIs carries the numeric measurements per capsule.
type KPIs struct {
	ThroughputRPS  float64 `json:"throughput_rps"`
	P95Ms          float64 `json:"p95_ms"`
	ErrorRate      float64 `json:"error_rate"`
	OOMAlarms      int     `json:"oom_alarms"`
	GoroutineCount int     `json:"goroutine_count"`
	GCPauseP99Us   float64 `json:"gc_pause_p99_us"`
	HeapInUseBytes uint64  `json:"heap_in_use_bytes"`

	// v3.1.0 EC-1-3 China Sourcing Agent KPIs. Emitted as additive
	// fields so prior schema readers keep working. Zero values are
	// the natural default when no sourcing has run in the window.
	SourcingRunsTotal              int     `json:"sourcing_runs_total,omitempty"`
	SourcingComplianceRejectsTotal int     `json:"sourcing_compliance_rejects_total,omitempty"`
	SourcingP95Ms                  float64 `json:"sourcing_p95_ms,omitempty"`
	SupplierScoreMean              float64 `json:"supplier_score_mean,omitempty"`

	// v3.2.0 EC-2 Enrichment Pipeline KPIs. Additive fields so prior
	// readers keep working. Stage label values mirror
	// observability.EnrichmentStage* (description_gen, image_gen,
	// seo_inject, wc_sync, trend_ingest).
	EnrichmentRunsTotal          int     `json:"enrichment_runs_total,omitempty"`
	EnrichmentFailuresTotal      int     `json:"enrichment_failures_total,omitempty"`
	EnrichmentP95Ms              float64 `json:"enrichment_p95_ms,omitempty"`
	EnrichmentQualityScoreMean   float64 `json:"enrichment_quality_score_mean,omitempty"`
	ImageProcessingTotal         int     `json:"image_processing_total,omitempty"`
	ImageProcessingFailuresTotal int     `json:"image_processing_failures_total,omitempty"`
	TrendIngestRecordsTotal      int     `json:"trend_ingest_records_total,omitempty"`
	SEOKeywordInjectsTotal       int     `json:"seo_keyword_injects_total,omitempty"`

	// v3.3.0 EC-3 TikTok Shop integration KPIs. Additive so prior
	// schema readers keep working. Surface from the v3.3.0
	// observability spine (internal/observability/tiktok_metrics.go)
	// + emitted per-minute by the sampler driver.
	TikTokAPICallsTotal          int     `json:"tiktok_api_calls_total,omitempty"`
	TikTokAPIFailuresTotal       int     `json:"tiktok_api_failures_total,omitempty"`
	TikTokAPIP95Ms               float64 `json:"tiktok_api_p95_ms,omitempty"`
	TikTokListingPublishedTotal  int     `json:"tiktok_listing_published_total,omitempty"`
	TikTokListingRolledBackTotal int     `json:"tiktok_listing_rolled_back_total,omitempty"`
	TikTokWebhookReceivedTotal   int     `json:"tiktok_webhook_received_total,omitempty"`
	TikTokWebhookSignatureFails  int     `json:"tiktok_webhook_signature_fails,omitempty"`
	TikTokInventorySyncTotal     int     `json:"tiktok_inventory_sync_total,omitempty"`
	TikTokInventorySyncRollbacks int     `json:"tiktok_inventory_sync_rollbacks,omitempty"`

	// v3.4.0 EC-4 + EC-5 cross-platform channel + content cluster
	// KPIs. Additive so prior schema readers keep working.
	FacebookAPICallsTotal        int     `json:"facebook_api_calls_total,omitempty"`
	FacebookAPIFailuresTotal     int     `json:"facebook_api_failures_total,omitempty"`
	ChannelRouterDispatchesTotal int     `json:"channel_router_dispatches_total,omitempty"`
	ChannelRouterDLQTotal        int     `json:"channel_router_dlq_total,omitempty"`
	RedNoteBridgeCallsTotal      int     `json:"rednote_bridge_calls_total,omitempty"`
	VideoScriptGenerationsTotal  int     `json:"video_script_generations_total,omitempty"`
	VideoScriptQualityScoreMean  float64 `json:"video_script_quality_score_mean,omitempty"`
	VideoAssemblyTotal           int     `json:"video_assembly_total,omitempty"`
	VideoAssemblyFailuresTotal   int     `json:"video_assembly_failures_total,omitempty"`

	// v3.5.0 EC-6 + EC-7 pricing + fulfilment seed KPIs. Additive
	// so prior schema readers keep working. Surface from the
	// v3.5.0 observability spine + emitted per-minute by the
	// sampler driver.
	SupplierCostChangesTotal           int     `json:"supplier_cost_changes_total,omitempty"`
	PricingDecisionsTotal              int     `json:"pricing_decisions_total,omitempty"`
	OrderAggregatorNormalisationsTotal int     `json:"order_aggregator_normalisations_total,omitempty"`
	DropshipOrdersTotal                int     `json:"dropship_orders_total,omitempty"`
	FXRateAgeMaxSeconds                float64 `json:"fx_rate_age_max_seconds,omitempty"`

	// v3.6.0 EC-8 + EC-9 customer service + analytics KPIs.
	// Additive so prior schema readers keep working. Surface from
	// the v3.6.0 observability spine + emitted per-minute by the
	// sampler driver. Mirrors the EC-3/4/5 additive pattern.
	EnquiryClassificationsTotal int     `json:"enquiry_classifications_total,omitempty"`
	FAQResponsesTotal           int     `json:"faq_responses_total,omitempty"`
	MessageWebhookReceivedTotal int     `json:"message_webhook_received_total,omitempty"`
	GMVP95LatencyMs             float64 `json:"gmv_p95_latency_ms,omitempty"`
	SSEActiveConnectionsMax     int     `json:"sse_active_connections_max,omitempty"`

	// v3.7.0 EC-10 uiauto hardening KPIs. Additive so prior schema
	// readers keep working. Surface from the v3.7.0 observability
	// spine (internal/metrics + internal/uiauto/{session,memguard,
	// ratelimit,captcha}) + emitted per-minute by the sampler driver.
	UIAutoSessionOpsTotal       int     `json:"uiauto_session_ops_total,omitempty"`
	OmniParserInferenceP95Ms    float64 `json:"omniparser_inference_p95_ms,omitempty"`
	OmniParserMemoryPausesTotal int     `json:"omniparser_memory_pauses_total,omitempty"`
	UIAutoRateLimitDropsTotal   int     `json:"uiauto_rate_limit_drops_total,omitempty"`
	CAPTCHADetectionsTotal      int     `json:"captcha_detections_total,omitempty"`
	CAPTCHAAvgResolutionSeconds float64 `json:"captcha_avg_resolution_seconds,omitempty"`

	// v3.9.0 EC-6-4 + EC-6-5 + EC-5-2 + EC-5-4 + EC-5-5 +
	// carry-forward KPIs. Additive so prior schema readers keep
	// working. Surface from the v3.9.0 observability spine
	// (internal/observability/v390_metrics.go) + emitted per-minute
	// by the sampler driver.
	CompetitorUndercutsDetectedTotal int     `json:"competitor_undercuts_detected_total,omitempty"`
	MarginDashboardP95Ms             float64 `json:"margin_dashboard_p95_ms,omitempty"`
	ContentCalendarPublishingP95Ms   float64 `json:"content_calendar_publishing_p95_ms,omitempty"`
	HashtagCaptionAvgScore           float64 `json:"hashtag_caption_avg_score,omitempty"`
	ContentEMAMaxScorePerChannel     float64 `json:"content_ema_max_score_per_channel,omitempty"`
	ChannelStatusUpdatesTotal        int     `json:"channel_status_updates_total,omitempty"`

	// v3.9.1 Existing #10 + EC-9-4 + EC-9-5 + EC-4-4 KPIs. Additive
	// so prior schema readers keep working. Surface from the v3.9.1
	// observability spine (internal/observability/v391_metrics.go).
	OnboardingWizardsCompletedTotal int     `json:"onboarding_wizards_completed_total,omitempty"`
	ChannelContentP95Ms             float64 `json:"channel_content_p95_ms,omitempty"`
	OperatorAlertsPendingTotal      int     `json:"operator_alerts_pending_total,omitempty"`
	StubChannelCallsTotal           int     `json:"stub_channel_calls_total,omitempty"`

	// v4.11.0 Agentrace deep-integration KPIs. Additive so prior
	// schema readers keep working. Populated by AgentraceAdapter
	// from cursor-tools agentrace loopback (HTTP) or JSONL fallback.
	AgentraceAvailable          bool    `json:"agentrace_available,omitempty"`
	AgentraceSessionDurationSec float64 `json:"agentrace_session_duration_seconds,omitempty"`
	AgentraceToolCallCount      int     `json:"agentrace_tool_call_count,omitempty"`
	AgentraceCostUSD            float64 `json:"agentrace_cost_usd,omitempty"`
	AgentraceBottleneckCount    int     `json:"agentrace_bottleneck_count,omitempty"`
	AgentraceParallelismRatio   float64 `json:"agentrace_parallelism_efficiency,omitempty"`
}

// Config controls Sink construction.
type Config struct {
	Path   string           // base file path
	Binary string           // default Binary on capsules
	Rotate bool             // enable daily rotation by ISO date suffix
	Now    func() time.Time // injectable clock for tests
}

// Sink writes Capsules to disk one per line.
type Sink struct {
	cfg    Config
	logger *slog.Logger

	mu         sync.Mutex
	file       *os.File
	writer     *bufio.Writer
	currentDay string
	closed     bool
}

// NewSink opens or creates the NDJSON file. Parent directories are
// created if missing.
func NewSink(logger *slog.Logger, cfg Config) (*Sink, error) {
	if cfg.Path == "" {
		return nil, fmt.Errorf("evomap: sink path required")
	}
	if cfg.Binary == "" {
		cfg.Binary = "unknown"
	}
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if logger == nil {
		logger = slog.Default()
	}
	s := &Sink{cfg: cfg, logger: logger}
	if err := s.rotateIfNeeded(cfg.Now()); err != nil {
		return nil, err
	}
	return s, nil
}

// Write appends a Capsule. If the binary is unset on the capsule the
// Sink default is applied; same for RecordedAt (now).
func (s *Sink) Write(ctx context.Context, c Capsule) error {
	if ctx.Err() != nil {
		return ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return fmt.Errorf("evomap: sink closed")
	}
	now := s.cfg.Now()
	if c.Binary == "" {
		c.Binary = s.cfg.Binary
	}
	if c.RecordedAt.IsZero() {
		c.RecordedAt = now
	}
	if c.EventAt.IsZero() {
		c.EventAt = now
	}
	if err := s.rotateIfNeededLocked(now); err != nil {
		return err
	}
	if err := writeJSONLine(s.writer, c); err != nil {
		return err
	}
	return s.writer.Flush()
}

// Close flushes and closes the underlying file. Implements lifecycle.Closer.
func (s *Sink) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}
	s.closed = true
	if s.writer != nil {
		_ = s.writer.Flush()
	}
	if s.file != nil {
		err := s.file.Close()
		s.file = nil
		s.writer = nil
		return err
	}
	return nil
}

func (s *Sink) rotateIfNeeded(now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.rotateIfNeededLocked(now)
}

func (s *Sink) rotateIfNeededLocked(now time.Time) error {
	day := now.UTC().Format("2006-01-02")
	if s.file != nil && (!s.cfg.Rotate || day == s.currentDay) {
		return nil
	}
	if s.file != nil {
		_ = s.writer.Flush()
		_ = s.file.Close()
	}
	path := s.cfg.Path
	if s.cfg.Rotate {
		dir, base := filepath.Split(path)
		ext := filepath.Ext(base)
		stem := strings.TrimSuffix(base, ext)
		path = filepath.Join(dir, fmt.Sprintf("%s-%s%s", stem, day, ext))
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("evomap: mkdir: %w", err)
	}
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("evomap: open: %w", err)
	}
	s.file = f
	s.writer = bufio.NewWriter(f)
	s.currentDay = day
	return nil
}

func writeJSONLine(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}

// AggregateResult is the daily aggregation output produced by
// cmd/evomap-rollup. Pure value type so tests are trivial.
type AggregateResult struct {
	SampleCount        int
	MeanThroughputRPS  float64
	MaxP95Ms           float64
	MeanErrorRate      float64
	TotalOOMAlarms     int
	MaxGoroutineCount  int
	MaxHeapInUseBytes  uint64
	MeanGCPauseP99Us   float64
	WindowStart        time.Time
	WindowEnd          time.Time
	BinaryDistribution map[string]int

	// v3.1.0 EC-1-3 sourcing KPIs aggregated into the daily roll-up.
	TotalSourcingRuns              int
	TotalSourcingComplianceRejects int
	MaxSourcingP95Ms               float64
	MeanSupplierScore              float64

	// v3.2.0 EC-2 enrichment KPIs aggregated into the daily roll-up.
	TotalEnrichmentRuns          int
	TotalEnrichmentFailures      int
	MaxEnrichmentP95Ms           float64
	MeanEnrichmentQualityScore   float64
	TotalImageProcessing         int
	TotalImageProcessingFailures int
	TotalTrendIngestRecords      int
	TotalSEOKeywordInjects       int

	// v3.3.0 EC-3 TikTok integration KPIs aggregated into the
	// daily roll-up. Operator + EvoLoop dashboards pivot on these.
	TotalTikTokAPICalls           int
	TotalTikTokAPIFailures        int
	MaxTikTokAPIP95Ms             float64
	TotalTikTokListingPublished   int
	TotalTikTokListingRolledBack  int
	TotalTikTokWebhookReceived    int
	TotalTikTokWebhookSigFailures int
	TotalTikTokInventorySync      int
	TotalTikTokInventoryRollbacks int

	// v3.4.0 EC-4 + EC-5 KPIs aggregated into the daily roll-up.
	TotalFacebookAPICalls        int
	TotalFacebookAPIFailures     int
	TotalChannelRouterDispatches int
	TotalChannelRouterDLQ        int
	TotalRedNoteBridgeCalls      int
	TotalVideoScriptGenerations  int
	MeanVideoScriptQualityScore  float64
	TotalVideoAssembly           int
	TotalVideoAssemblyFailures   int

	// v3.5.0 EC-6 + EC-7 pricing + fulfilment seed KPIs aggregated
	// into the daily roll-up.
	TotalSupplierCostChanges           int
	TotalPricingDecisions              int
	TotalOrderAggregatorNormalisations int
	TotalDropshipOrders                int
	MaxFXRateAgeSeconds                float64

	// v3.6.0 EC-8 + EC-9 KPIs aggregated into the daily roll-up.
	TotalEnquiryClassifications int
	TotalFAQResponses           int
	TotalMessageWebhookReceived int
	MaxGMVP95LatencyMs          float64
	MaxSSEActiveConnections     int

	// v3.7.0 EC-10 uiauto hardening KPIs aggregated into the daily
	// roll-up. Operator + EvoLoop dashboards pivot on these.
	TotalUIAutoSessionOps        int
	MaxOmniParserInferenceP95Ms  float64
	TotalOmniParserMemoryPauses  int
	TotalUIAutoRateLimitDrops    int
	TotalCAPTCHADetections       int
	MeanCAPTCHAResolutionSeconds float64

	// v3.9.0 EC-6-4 + EC-6-5 + EC-5-2 + EC-5-4 + EC-5-5 +
	// carry-forward KPIs aggregated into the daily roll-up.
	TotalCompetitorUndercutsDetected  int
	MaxMarginDashboardP95Ms           float64
	MaxContentCalendarPublishingP95Ms float64
	MeanHashtagCaptionScore           float64
	MaxContentEMAScorePerChannel      float64
	TotalChannelStatusUpdates         int

	// v3.9.1 Existing #10 + EC-9-4 + EC-9-5 + EC-4-4 KPIs aggregated
	// into the daily roll-up.
	TotalOnboardingWizardsCompleted int
	MaxChannelContentP95Ms          float64
	MaxOperatorAlertsPending        int
	TotalStubChannelCalls           int
}

// aggregateAccumulator carries the per-iteration running sums + sample
// counts so the per-epoch loop body can split into focused helpers and
// keep individual function cyclomatic complexity well under the v3.1.0
// sentrux ceiling.
type aggregateAccumulator struct {
	sumRPS                      float64
	sumErr                      float64
	sumGC                       float64
	sumSupplierScore            float64
	sumEnrichmentQuality        float64
	sumVideoScriptQuality       float64
	sumCAPTCHAResolutionSeconds float64
	sumHashtagCaptionScore      float64
	supplierScoreSamples        int
	enrichmentQualitySamples    int
	videoScriptQualitySamples   int
	captchaResolutionSamples    int
	hashtagCaptionScoreSamples  int
}

// Aggregate computes summary KPIs across a slice of capsules.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4): the per-capsule body splits into focused helpers
// (foundationals, sourcing, enrichment, tiktok, channel-content)
// so this composer stays well under the cyclomatic ceiling.
func Aggregate(caps []Capsule) AggregateResult {
	res := AggregateResult{BinaryDistribution: map[string]int{}}
	if len(caps) == 0 {
		return res
	}
	res.SampleCount = len(caps)
	res.WindowStart = caps[0].EventAt
	res.WindowEnd = caps[0].EventAt
	var acc aggregateAccumulator
	for _, c := range caps {
		accumulateFoundational(c, &res, &acc)
		accumulateSourcing(c, &res, &acc)
		accumulateEnrichment(c, &res, &acc)
		accumulateTikTok(c, &res)
		accumulateChannelContent(c, &res, &acc)
		accumulatePricingFulfilment(c, &res)
		accumulateCustomerServiceAnalytics(c, &res)
		accumulateUIAutoHardening(c, &res, &acc)
		accumulateV390(c, &res, &acc)
		accumulateV391(c, &res)
	}
	n := float64(len(caps))
	res.MeanThroughputRPS = acc.sumRPS / n
	res.MeanErrorRate = acc.sumErr / n
	res.MeanGCPauseP99Us = acc.sumGC / n
	if acc.supplierScoreSamples > 0 {
		res.MeanSupplierScore = acc.sumSupplierScore / float64(acc.supplierScoreSamples)
	}
	if acc.enrichmentQualitySamples > 0 {
		res.MeanEnrichmentQualityScore = acc.sumEnrichmentQuality / float64(acc.enrichmentQualitySamples)
	}
	if acc.videoScriptQualitySamples > 0 {
		res.MeanVideoScriptQualityScore = acc.sumVideoScriptQuality / float64(acc.videoScriptQualitySamples)
	}
	if acc.captchaResolutionSamples > 0 {
		res.MeanCAPTCHAResolutionSeconds = acc.sumCAPTCHAResolutionSeconds / float64(acc.captchaResolutionSamples)
	}
	if acc.hashtagCaptionScoreSamples > 0 {
		res.MeanHashtagCaptionScore = acc.sumHashtagCaptionScore / float64(acc.hashtagCaptionScoreSamples)
	}
	return res
}

// accumulateFoundational rolls in the throughput / error / heap /
// goroutine / window-bounds fields shared by every binary.
func accumulateFoundational(c Capsule, res *AggregateResult, acc *aggregateAccumulator) {
	acc.sumRPS += c.KPIs.ThroughputRPS
	acc.sumErr += c.KPIs.ErrorRate
	acc.sumGC += c.KPIs.GCPauseP99Us
	if c.KPIs.P95Ms > res.MaxP95Ms {
		res.MaxP95Ms = c.KPIs.P95Ms
	}
	if c.KPIs.GoroutineCount > res.MaxGoroutineCount {
		res.MaxGoroutineCount = c.KPIs.GoroutineCount
	}
	if c.KPIs.HeapInUseBytes > res.MaxHeapInUseBytes {
		res.MaxHeapInUseBytes = c.KPIs.HeapInUseBytes
	}
	res.TotalOOMAlarms += c.KPIs.OOMAlarms
	res.BinaryDistribution[c.Binary]++
	if c.EventAt.Before(res.WindowStart) {
		res.WindowStart = c.EventAt
	}
	if c.EventAt.After(res.WindowEnd) {
		res.WindowEnd = c.EventAt
	}
}

// accumulateSourcing rolls in the v3.1.0 EC-1 China sourcing KPIs.
func accumulateSourcing(c Capsule, res *AggregateResult, acc *aggregateAccumulator) {
	res.TotalSourcingRuns += c.KPIs.SourcingRunsTotal
	res.TotalSourcingComplianceRejects += c.KPIs.SourcingComplianceRejectsTotal
	if c.KPIs.SourcingP95Ms > res.MaxSourcingP95Ms {
		res.MaxSourcingP95Ms = c.KPIs.SourcingP95Ms
	}
	if c.KPIs.SupplierScoreMean > 0 {
		acc.sumSupplierScore += c.KPIs.SupplierScoreMean
		acc.supplierScoreSamples++
	}
}

// accumulateEnrichment rolls in the v3.2.0 EC-2 enrichment KPIs.
func accumulateEnrichment(c Capsule, res *AggregateResult, acc *aggregateAccumulator) {
	res.TotalEnrichmentRuns += c.KPIs.EnrichmentRunsTotal
	res.TotalEnrichmentFailures += c.KPIs.EnrichmentFailuresTotal
	if c.KPIs.EnrichmentP95Ms > res.MaxEnrichmentP95Ms {
		res.MaxEnrichmentP95Ms = c.KPIs.EnrichmentP95Ms
	}
	if c.KPIs.EnrichmentQualityScoreMean > 0 {
		acc.sumEnrichmentQuality += c.KPIs.EnrichmentQualityScoreMean
		acc.enrichmentQualitySamples++
	}
	res.TotalImageProcessing += c.KPIs.ImageProcessingTotal
	res.TotalImageProcessingFailures += c.KPIs.ImageProcessingFailuresTotal
	res.TotalTrendIngestRecords += c.KPIs.TrendIngestRecordsTotal
	res.TotalSEOKeywordInjects += c.KPIs.SEOKeywordInjectsTotal
}

// accumulateTikTok rolls in the v3.3.0 EC-3 TikTok integration KPIs.
func accumulateTikTok(c Capsule, res *AggregateResult) {
	res.TotalTikTokAPICalls += c.KPIs.TikTokAPICallsTotal
	res.TotalTikTokAPIFailures += c.KPIs.TikTokAPIFailuresTotal
	if c.KPIs.TikTokAPIP95Ms > res.MaxTikTokAPIP95Ms {
		res.MaxTikTokAPIP95Ms = c.KPIs.TikTokAPIP95Ms
	}
	res.TotalTikTokListingPublished += c.KPIs.TikTokListingPublishedTotal
	res.TotalTikTokListingRolledBack += c.KPIs.TikTokListingRolledBackTotal
	res.TotalTikTokWebhookReceived += c.KPIs.TikTokWebhookReceivedTotal
	res.TotalTikTokWebhookSigFailures += c.KPIs.TikTokWebhookSignatureFails
	res.TotalTikTokInventorySync += c.KPIs.TikTokInventorySyncTotal
	res.TotalTikTokInventoryRollbacks += c.KPIs.TikTokInventorySyncRollbacks
}

// accumulateChannelContent rolls in the v3.4.0 EC-4 + EC-5 channel
// router + content cluster KPIs.
func accumulateChannelContent(c Capsule, res *AggregateResult, acc *aggregateAccumulator) {
	res.TotalFacebookAPICalls += c.KPIs.FacebookAPICallsTotal
	res.TotalFacebookAPIFailures += c.KPIs.FacebookAPIFailuresTotal
	res.TotalChannelRouterDispatches += c.KPIs.ChannelRouterDispatchesTotal
	res.TotalChannelRouterDLQ += c.KPIs.ChannelRouterDLQTotal
	res.TotalRedNoteBridgeCalls += c.KPIs.RedNoteBridgeCallsTotal
	res.TotalVideoScriptGenerations += c.KPIs.VideoScriptGenerationsTotal
	if c.KPIs.VideoScriptQualityScoreMean > 0 {
		acc.sumVideoScriptQuality += c.KPIs.VideoScriptQualityScoreMean
		acc.videoScriptQualitySamples++
	}
	res.TotalVideoAssembly += c.KPIs.VideoAssemblyTotal
	res.TotalVideoAssemblyFailures += c.KPIs.VideoAssemblyFailuresTotal
}

// accumulatePricingFulfilment rolls in the v3.5.0 EC-6 + EC-7
// pricing + fulfilment seed KPIs.
func accumulatePricingFulfilment(c Capsule, res *AggregateResult) {
	res.TotalSupplierCostChanges += c.KPIs.SupplierCostChangesTotal
	res.TotalPricingDecisions += c.KPIs.PricingDecisionsTotal
	res.TotalOrderAggregatorNormalisations += c.KPIs.OrderAggregatorNormalisationsTotal
	res.TotalDropshipOrders += c.KPIs.DropshipOrdersTotal
	if c.KPIs.FXRateAgeMaxSeconds > res.MaxFXRateAgeSeconds {
		res.MaxFXRateAgeSeconds = c.KPIs.FXRateAgeMaxSeconds
	}
}

// accumulateCustomerServiceAnalytics rolls in the v3.6.0 EC-8 +
// EC-9 customer service + analytics KPIs.
func accumulateCustomerServiceAnalytics(c Capsule, res *AggregateResult) {
	res.TotalEnquiryClassifications += c.KPIs.EnquiryClassificationsTotal
	res.TotalFAQResponses += c.KPIs.FAQResponsesTotal
	res.TotalMessageWebhookReceived += c.KPIs.MessageWebhookReceivedTotal
	if c.KPIs.GMVP95LatencyMs > res.MaxGMVP95LatencyMs {
		res.MaxGMVP95LatencyMs = c.KPIs.GMVP95LatencyMs
	}
	if c.KPIs.SSEActiveConnectionsMax > res.MaxSSEActiveConnections {
		res.MaxSSEActiveConnections = c.KPIs.SSEActiveConnectionsMax
	}
}

// accumulateUIAutoHardening rolls in the v3.7.0 EC-10 uiauto
// hardening KPIs (session ops, OmniParser inference latency,
// memory-pressure pauses, rate-limit drops, CAPTCHA detections,
// CAPTCHA average resolution).
func accumulateUIAutoHardening(c Capsule, res *AggregateResult, acc *aggregateAccumulator) {
	res.TotalUIAutoSessionOps += c.KPIs.UIAutoSessionOpsTotal
	if c.KPIs.OmniParserInferenceP95Ms > res.MaxOmniParserInferenceP95Ms {
		res.MaxOmniParserInferenceP95Ms = c.KPIs.OmniParserInferenceP95Ms
	}
	res.TotalOmniParserMemoryPauses += c.KPIs.OmniParserMemoryPausesTotal
	res.TotalUIAutoRateLimitDrops += c.KPIs.UIAutoRateLimitDropsTotal
	res.TotalCAPTCHADetections += c.KPIs.CAPTCHADetectionsTotal
	if c.KPIs.CAPTCHAAvgResolutionSeconds > 0 {
		acc.sumCAPTCHAResolutionSeconds += c.KPIs.CAPTCHAAvgResolutionSeconds
		acc.captchaResolutionSamples++
	}
}

// accumulateV391 rolls in the v3.9.1 Existing #10 + EC-9-4 + EC-9-5
// + EC-4-4 KPIs. Cyclomatic 4.
func accumulateV391(c Capsule, res *AggregateResult) {
	res.TotalOnboardingWizardsCompleted += c.KPIs.OnboardingWizardsCompletedTotal
	if c.KPIs.ChannelContentP95Ms > res.MaxChannelContentP95Ms {
		res.MaxChannelContentP95Ms = c.KPIs.ChannelContentP95Ms
	}
	if c.KPIs.OperatorAlertsPendingTotal > res.MaxOperatorAlertsPending {
		res.MaxOperatorAlertsPending = c.KPIs.OperatorAlertsPendingTotal
	}
	res.TotalStubChannelCalls += c.KPIs.StubChannelCallsTotal
}

// accumulateV390 rolls in the v3.9.0 EC-6-4 + EC-6-5 + EC-5-2 +
// EC-5-4 + EC-5-5 + carry-forward KPIs.
func accumulateV390(c Capsule, res *AggregateResult, acc *aggregateAccumulator) {
	res.TotalCompetitorUndercutsDetected += c.KPIs.CompetitorUndercutsDetectedTotal
	if c.KPIs.MarginDashboardP95Ms > res.MaxMarginDashboardP95Ms {
		res.MaxMarginDashboardP95Ms = c.KPIs.MarginDashboardP95Ms
	}
	if c.KPIs.ContentCalendarPublishingP95Ms > res.MaxContentCalendarPublishingP95Ms {
		res.MaxContentCalendarPublishingP95Ms = c.KPIs.ContentCalendarPublishingP95Ms
	}
	if c.KPIs.HashtagCaptionAvgScore > 0 {
		acc.sumHashtagCaptionScore += c.KPIs.HashtagCaptionAvgScore
		acc.hashtagCaptionScoreSamples++
	}
	if c.KPIs.ContentEMAMaxScorePerChannel > res.MaxContentEMAScorePerChannel {
		res.MaxContentEMAScorePerChannel = c.KPIs.ContentEMAMaxScorePerChannel
	}
	res.TotalChannelStatusUpdates += c.KPIs.ChannelStatusUpdatesTotal
}
