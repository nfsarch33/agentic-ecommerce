// Package metrics implements the v2.10.0 ec_* Prometheus metric
// registry. The package emits Prometheus text format directly so we
// avoid adding github.com/prometheus/client_golang and stay aligned
// with the existing handcrafted exposition format used elsewhere in
// the repo (cmd/mc-api metricsHandler).
//
// The registry is intentionally small (~300 LOC). It supports:
//
//   - Counter, Gauge, Histogram with label sets.
//   - A bounded cardinality cap per metric (default 10_000) so a
//     single hot path cannot OOM the registry.
//   - A /metrics handler implementing the Prometheus content-type.
//
// Cite skill: monitoring-observability for the Four Golden Signals
// (the http + workflow + workerpool + memwatch metrics map directly).
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

// Labels is the dimension set attached to every metric observation.
// Keys + values must already be Prometheus-safe (no quotes, newlines).
type Labels map[string]string

// Option configures a Registry at construction time.
type Option func(*Registry)

// WithMaxSeries caps the number of distinct label combinations a
// single metric can hold before further observations are dropped (a
// safety net against label cardinality explosions).
func WithMaxSeries(max int) Option {
	return func(r *Registry) {
		if max > 0 {
			r.maxSeries = max
		}
	}
}

// Registry is the per-binary metric collection.
type Registry struct {
	binary    string
	maxSeries int

	dropped atomic.Int64

	HTTPRequests         *Counter
	HTTPDuration         *Histogram
	WorkflowRuns         *Counter
	WorkflowDuration     *Histogram
	WorkerpoolQueued     *Gauge
	WorkerpoolSaturation *Counter
	OOMAlarms            *Counter
	GoroutineCount       *Gauge
	HeapBytes            *Gauge

	// v3.1.0 EC-1-3 China Sourcing Agent metrics. Cardinality budget:
	// ec_sourcing_runs_total{tenant_id, source} -- bounded by tenants
	// (~10) x sources (2: 1688, taobao) = 20 series.
	// ec_sourcing_duration_seconds{source} -- 2 series.
	// ec_sourcing_compliance_rejects_total{category} -- bounded by
	// the compliance.auImportRestricted + platformProhibited maps
	// (~25 distinct categories).
	// ec_supplier_score_distribution -- 1 series (no labels).
	SourcingRuns              *Counter
	SourcingDuration          *Histogram
	SourcingComplianceRejects *Counter
	SupplierScoreDistribution *Histogram

	// v3.2.0 EC-2 AI Product Enrichment Pipeline metrics.
	// Cardinality budget per series:
	//   ec_enrichment_runs_total{tenant_id, stage, status}
	//     ~ tenants(10) * stages(4) * statuses(2) = 80 series.
	//   ec_enrichment_duration_seconds{stage}        ~ 4 series.
	//   ec_enrichment_quality_score{stage}            ~ 4 series.
	//   ec_image_processing_total{action, status}    ~ actions(2) * statuses(4) = 8 series.
	//   ec_trend_ingest_records_total{platform}      ~ 3 series.
	//   ec_seo_keyword_injects_total{tenant_id}      ~ 10 series.
	// Total ~ 109 series additive for v3.2.0; well under the
	// per-binary 10_000 cap.
	EnrichmentRuns          *Counter
	EnrichmentDuration      *Histogram
	EnrichmentQualityScore  *Histogram
	ImageProcessingTotal    *Counter
	ImageProcessingDuration *Histogram
	TrendIngestRecordsTotal *Counter
	SEOKeywordInjectsTotal  *Counter

	// v3.3.0 EC-3 TikTok Shop integration metrics. Cardinality
	// budget per series:
	//   ec_tiktok_api_calls_total{tenant_id, endpoint, status}
	//     ~ tenants(10) * endpoints(6: products.list/create/update/
	//       delete + inventory.sync + oauth) * statuses(8) = 480 series.
	//   ec_tiktok_api_duration_seconds{endpoint}     ~ 6 series.
	//   ec_tiktok_listing_attempts_total{outcome}    ~ outcomes(6:
	//     published, publish_failed, rolled_back, uiauto.ok,
	//     uiauto.bridge_rejected, uiauto.transport_error) = 6 series.
	//   ec_tiktok_webhook_received_total{event_type, status}
	//     ~ event_types(2: order, refund) * statuses(8) = 16 series.
	//   ec_tiktok_inventory_sync_total{direction, status}
	//     ~ directions(2) * statuses(7) = 14 series.
	//   ec_tiktok_signature_failures_total{reason}   ~ reasons(5:
	//     missing, mismatch, malformed, expired, other) = 5 series.
	// Total ~ 527 additive series for v3.3.0; well under the
	// per-binary 10_000 cap.
	TikTokAPICallsTotal          *Counter
	TikTokAPIDurationSeconds     *Histogram
	TikTokListingAttemptsTotal   *Counter
	TikTokWebhookReceivedTotal   *Counter
	TikTokInventorySyncTotal     *Counter
	TikTokSignatureFailuresTotal *Counter

	// v3.4.0 EC-4 + EC-5 cross-platform channel + content metrics.
	// Cardinality budget per series:
	//   ec_facebook_api_calls_total{tenant_id, endpoint, status}
	//     ~ tenants(10) * endpoints(5: catalog.products.create/batch +
	//       inventory.sync + commerce.orders.status + oauth) *
	//       statuses(7) = 350 series.
	//   ec_facebook_api_duration_seconds{endpoint}   ~ 5 series.
	//   ec_channel_router_dispatches_total{tenant_id, channel, outcome}
	//     ~ tenants(10) * channels(5: tiktok/facebook/rednote/
	//       instagram/(none)) * outcomes(3: delivered/dlq/no_match)
	//       = 150 series.
	//   ec_channel_router_dlq_total{tenant_id, channel, reason}
	//     ~ tenants(10) * channels(4) * reasons(4: pool_saturated,
	//       publish_failed, transport_error, decode_failed) = 160 series.
	//   ec_rednote_bridge_calls_total{tenant_id, status}
	//     ~ tenants(10) * statuses(8) = 80 series.
	//   ec_video_script_generations_total{tenant_id, platform, source}
	//     ~ tenants(10) * platforms(4: tiktok/rednote/facebook/
	//       instagram-reels) * sources(2: llm/template) = 80 series.
	//   ec_video_script_quality_score{platform}      ~ 4 series.
	//   ec_video_assembly_total{action, status}      ~ 2 actions
	//     (stub_assemble/live_assemble) * statuses(2: ok/failed) = 4 series.
	// Total ~ 833 additive series for v3.4.0; well under the
	// per-binary 10_000 cap.
	FacebookAPICallsTotal        *Counter
	FacebookAPIDurationSeconds   *Histogram
	ChannelRouterDispatchesTotal *Counter
	ChannelRouterDLQTotal        *Counter
	RedNoteBridgeCallsTotal      *Counter
	VideoScriptGenerationsTotal  *Counter
	VideoScriptQualityScore      *Histogram
	VideoAssemblyTotal           *Counter
}

// NewRegistry returns a Registry pre-populated with the v2.10.0
// ec_* metric set.
func NewRegistry(binary string, opts ...Option) *Registry {
	r := &Registry{
		binary:    binary,
		maxSeries: 10_000,
	}
	for _, opt := range opts {
		opt(r)
	}
	r.HTTPRequests = newCounter(r, "ec_http_requests_total", "HTTP request count emitted by mc-api and worker /metrics endpoints.")
	r.HTTPDuration = newHistogram(r, "ec_http_duration_seconds", "HTTP request duration histogram.", defaultDurationBuckets)
	r.WorkflowRuns = newCounter(r, "ec_workflow_runs_total", "Temporal workflow runs.")
	r.WorkflowDuration = newHistogram(r, "ec_workflow_duration_seconds", "Workflow duration histogram.", defaultDurationBuckets)
	r.WorkerpoolQueued = newGauge(r, "ec_workerpool_queued", "Outstanding tasks queued per workerpool.")
	r.WorkerpoolSaturation = newCounter(r, "ec_workerpool_saturation_total", "Submit calls that returned ErrPoolSaturated.")
	r.OOMAlarms = newCounter(r, "ec_oom_alarms_total", "memwatch heap-ceiling breaches that fired the alarm callback.")
	r.GoroutineCount = newGauge(r, "ec_goroutine_count", "Sampled runtime.NumGoroutine.")
	r.HeapBytes = newGauge(r, "ec_heap_bytes", "Sampled runtime.MemStats.HeapInuse.")
	r.SourcingRuns = newCounter(r, "ec_sourcing_runs_total", "v3.1.0 China sourcing agent runs by tenant + source.")
	r.SourcingDuration = newHistogram(r, "ec_sourcing_duration_seconds", "China sourcing agent run duration by source.", defaultDurationBuckets)
	r.SourcingComplianceRejects = newCounter(r, "ec_sourcing_compliance_rejects_total", "Products rejected by the compliance gate by category.")
	r.SupplierScoreDistribution = newHistogram(r, "ec_supplier_score_distribution", "Distribution of supplier scores observed during sourcing.", defaultScoreBuckets)
	r.EnrichmentRuns = newCounter(r, "ec_enrichment_runs_total", "v3.2.0 AI product enrichment pipeline runs by tenant + stage + status.")
	r.EnrichmentDuration = newHistogram(r, "ec_enrichment_duration_seconds", "v3.2.0 enrichment pipeline duration by stage.", defaultDurationBuckets)
	r.EnrichmentQualityScore = newHistogram(r, "ec_enrichment_quality_score", "v3.2.0 enrichment pipeline output quality score by stage (0..1).", defaultScoreBuckets)
	r.ImageProcessingTotal = newCounter(r, "ec_image_processing_total", "v3.2.0 EC-2-2 image pipeline operations by action + status.")
	r.ImageProcessingDuration = newHistogram(r, "ec_image_processing_duration_seconds", "v3.2.0 EC-2-2 image pipeline duration by action.", defaultDurationBuckets)
	r.TrendIngestRecordsTotal = newCounter(r, "ec_trend_ingest_records_total", "v3.2.0 EC-2-4 trend records ingested by platform.")
	r.SEOKeywordInjectsTotal = newCounter(r, "ec_seo_keyword_injects_total", "v3.2.0 EC-2-3 SEO keyword injection runs by tenant.")
	r.TikTokAPICallsTotal = newCounter(r, "ec_tiktok_api_calls_total", "v3.3.0 EC-3-1 TikTok Shop API calls by tenant + endpoint + status.")
	r.TikTokAPIDurationSeconds = newHistogram(r, "ec_tiktok_api_duration_seconds", "v3.3.0 EC-3-1 TikTok Shop API duration by endpoint.", defaultDurationBuckets)
	r.TikTokListingAttemptsTotal = newCounter(r, "ec_tiktok_listing_attempts_total", "v3.3.0 EC-3-2 TikTok listing publish attempts by outcome.")
	r.TikTokWebhookReceivedTotal = newCounter(r, "ec_tiktok_webhook_received_total", "v3.3.0 EC-3-3 TikTok webhook events received by event_type + status.")
	r.TikTokInventorySyncTotal = newCounter(r, "ec_tiktok_inventory_sync_total", "v3.3.0 EC-3-4 TikTok inventory sync transitions by direction + status.")
	r.TikTokSignatureFailuresTotal = newCounter(r, "ec_tiktok_signature_failures_total", "v3.3.0 EC-3-1/EC-3-3 TikTok HMAC signature failures by reason.")
	r.FacebookAPICallsTotal = newCounter(r, "ec_facebook_api_calls_total", "v3.4.0 EC-4-2 Facebook Graph API calls by tenant + endpoint + status.")
	r.FacebookAPIDurationSeconds = newHistogram(r, "ec_facebook_api_duration_seconds", "v3.4.0 EC-4-2 Facebook Graph API duration by endpoint.", defaultDurationBuckets)
	r.ChannelRouterDispatchesTotal = newCounter(r, "ec_channel_router_dispatches_total", "v3.4.0 EC-4-3 channel router dispatches by tenant + channel + outcome.")
	r.ChannelRouterDLQTotal = newCounter(r, "ec_channel_router_dlq_total", "v3.4.0 EC-4-3 channel router DLQ enqueues by tenant + channel + reason.")
	r.RedNoteBridgeCallsTotal = newCounter(r, "ec_rednote_bridge_calls_total", "v3.4.0 EC-4-1 RedNote omniparser bridge calls by tenant + status.")
	r.VideoScriptGenerationsTotal = newCounter(r, "ec_video_script_generations_total", "v3.4.0 EC-5-1 video script generations by tenant + platform + source.")
	r.VideoScriptQualityScore = newHistogram(r, "ec_video_script_quality_score", "v3.4.0 EC-5-1 video script quality score by platform (0..1).", defaultScoreBuckets)
	r.VideoAssemblyTotal = newCounter(r, "ec_video_assembly_total", "v3.4.0 EC-5-3 video assembly operations by action + status.")
	return r
}

var defaultDurationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// defaultScoreBuckets is used by the supplier-score distribution
// histogram. Buckets cover the [0, 1] supplier-score range with
// 0.1-step granularity so reviewers can see the score distribution
// without label cardinality.
var defaultScoreBuckets = []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}

// Handler returns the http.Handler that exposes /metrics in
// Prometheus text format.
func (r *Registry) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		var sb strings.Builder
		r.HTTPRequests.write(&sb)
		r.HTTPDuration.write(&sb)
		r.WorkflowRuns.write(&sb)
		r.WorkflowDuration.write(&sb)
		r.WorkerpoolQueued.write(&sb)
		r.WorkerpoolSaturation.write(&sb)
		r.OOMAlarms.write(&sb)
		r.GoroutineCount.write(&sb)
		r.HeapBytes.write(&sb)
		r.SourcingRuns.write(&sb)
		r.SourcingDuration.write(&sb)
		r.SourcingComplianceRejects.write(&sb)
		r.SupplierScoreDistribution.write(&sb)
		r.EnrichmentRuns.write(&sb)
		r.EnrichmentDuration.write(&sb)
		r.EnrichmentQualityScore.write(&sb)
		r.ImageProcessingTotal.write(&sb)
		r.ImageProcessingDuration.write(&sb)
		r.TrendIngestRecordsTotal.write(&sb)
		r.SEOKeywordInjectsTotal.write(&sb)
		r.TikTokAPICallsTotal.write(&sb)
		r.TikTokAPIDurationSeconds.write(&sb)
		r.TikTokListingAttemptsTotal.write(&sb)
		r.TikTokWebhookReceivedTotal.write(&sb)
		r.TikTokInventorySyncTotal.write(&sb)
		r.TikTokSignatureFailuresTotal.write(&sb)
		r.FacebookAPICallsTotal.write(&sb)
		r.FacebookAPIDurationSeconds.write(&sb)
		r.ChannelRouterDispatchesTotal.write(&sb)
		r.ChannelRouterDLQTotal.write(&sb)
		r.RedNoteBridgeCallsTotal.write(&sb)
		r.VideoScriptGenerationsTotal.write(&sb)
		r.VideoScriptQualityScore.write(&sb)
		r.VideoAssemblyTotal.write(&sb)
		dropped := r.dropped.Load()
		if dropped > 0 {
			fmt.Fprintf(&sb, "# HELP ec_metrics_series_dropped_total Series rejected due to label cardinality cap.\n")
			fmt.Fprintf(&sb, "# TYPE ec_metrics_series_dropped_total counter\n")
			fmt.Fprintf(&sb, "ec_metrics_series_dropped_total{binary=%q} %d\n", r.binary, dropped)
		}
		_, _ = w.Write([]byte(sb.String()))
	})
}

// --- Counter ----------------------------------------------------------------

// Counter is a monotonically-increasing metric.
type Counter struct {
	r    *Registry
	name string
	help string

	mu     sync.Mutex
	values map[string]float64
}

func newCounter(r *Registry, name, help string) *Counter {
	return &Counter{r: r, name: name, help: help, values: map[string]float64{}}
}

// Inc adds 1 to the counter for the given label set.
func (c *Counter) Inc(l Labels) { c.Add(1, l) }

// Add increments the counter by delta. Negative deltas are rejected
// (Prometheus contract: counters are monotonic).
func (c *Counter) Add(delta float64, l Labels) {
	if delta < 0 {
		delta = 0
	}
	key := canonicalLabelKey(c.r.binary, l)
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.values[key]; !ok {
		if len(c.values) >= c.r.maxSeries {
			c.r.dropped.Add(1)
			return
		}
	}
	c.values[key] += delta
}

func (c *Counter) write(sb *strings.Builder) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.values) == 0 {
		return
	}
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	for _, key := range sortedKeys(c.values) {
		fmt.Fprintf(sb, "%s%s %g\n", c.name, key, c.values[key])
	}
}

// --- Gauge ------------------------------------------------------------------

// Gauge is a value that goes up and down (queue depths, current heap).
type Gauge struct {
	r    *Registry
	name string
	help string

	mu     sync.Mutex
	values map[string]float64
}

func newGauge(r *Registry, name, help string) *Gauge {
	return &Gauge{r: r, name: name, help: help, values: map[string]float64{}}
}

// Set replaces the gauge value for the label set.
func (g *Gauge) Set(v float64, l Labels) {
	key := canonicalLabelKey(g.r.binary, l)
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.values[key]; !ok {
		if len(g.values) >= g.r.maxSeries {
			g.r.dropped.Add(1)
			return
		}
	}
	g.values[key] = v
}

func (g *Gauge) write(sb *strings.Builder) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.values) == 0 {
		return
	}
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s gauge\n", g.name, g.help, g.name)
	for _, key := range sortedKeys(g.values) {
		fmt.Fprintf(sb, "%s%s %g\n", g.name, key, g.values[key])
	}
}

// --- Histogram --------------------------------------------------------------

// Histogram is a Prometheus histogram with bounded buckets.
type Histogram struct {
	r       *Registry
	name    string
	help    string
	buckets []float64

	mu     sync.Mutex
	series map[string]*histSeries
}

type histSeries struct {
	counts []uint64 // len(buckets)+1 (for +Inf)
	sum    float64
	count  uint64
}

func newHistogram(r *Registry, name, help string, buckets []float64) *Histogram {
	return &Histogram{r: r, name: name, help: help, buckets: buckets, series: map[string]*histSeries{}}
}

// Observe records a single observation.
func (h *Histogram) Observe(v float64, l Labels) {
	key := canonicalLabelKey(h.r.binary, l)
	h.mu.Lock()
	defer h.mu.Unlock()
	s, ok := h.series[key]
	if !ok {
		if len(h.series) >= h.r.maxSeries {
			h.r.dropped.Add(1)
			return
		}
		s = &histSeries{counts: make([]uint64, len(h.buckets)+1)}
		h.series[key] = s
	}
	for i, b := range h.buckets {
		if v <= b {
			s.counts[i]++
		}
	}
	s.counts[len(h.buckets)]++
	s.sum += v
	s.count++
}

func (h *Histogram) write(sb *strings.Builder) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.series) == 0 {
		return
	}
	fmt.Fprintf(sb, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	for _, key := range sortedKeys(h.series) {
		s := h.series[key]
		baseLabels := stripBraces(key)
		for i, b := range h.buckets {
			fmt.Fprintf(sb, "%s_bucket{%sle=\"%g\"} %d\n", h.name, baseLabels, b, s.counts[i])
		}
		fmt.Fprintf(sb, "%s_bucket{%sle=\"+Inf\"} %d\n", h.name, baseLabels, s.counts[len(h.buckets)])
		fmt.Fprintf(sb, "%s_sum%s %g\n", h.name, key, s.sum)
		fmt.Fprintf(sb, "%s_count%s %d\n", h.name, key, s.count)
	}
}

// --- helpers ----------------------------------------------------------------

func canonicalLabelKey(binary string, l Labels) string {
	keys := make([]string, 0, len(l)+1)
	out := make(Labels, len(l)+1)
	out["binary"] = binary
	for k, v := range l {
		out[k] = v
	}
	for k := range out {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	sb.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, `%s=%q`, k, out[k])
	}
	sb.WriteString("}")
	return sb.String()
}

// stripBraces returns the inner content of a {k=v,...} label key with
// a trailing comma so callers can append more labels (le="...").
func stripBraces(key string) string {
	if len(key) < 2 {
		return ""
	}
	inner := key[1 : len(key)-1]
	if inner == "" {
		return ""
	}
	return inner + ","
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
