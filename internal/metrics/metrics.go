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

	// v3.4.1 EC-4-5 channel health monitor metrics. Cardinality
	// budget per series:
	//   ec_channel_health_state{tenant_id, channel}
	//     ~ tenants(10) * channels(5) = 50 series.
	//   ec_channel_health_failure_rate{tenant_id, channel}
	//     ~ tenants(10) * channels(5) = 50 series.
	//   ec_channel_health_consecutive_failures{tenant_id, channel}
	//     ~ tenants(10) * channels(5) = 50 series.
	//   ec_channel_health_alerts_total{tenant_id, channel, state}
	//     ~ tenants(10) * channels(5) * states(2: degraded/
	//       unhealthy) = 100 series.
	//   ec_channel_health_recoveries_total{tenant_id, channel}
	//     ~ tenants(10) * channels(5) = 50 series.
	// Total ~ 300 additive series for v3.4.1; well under the
	// per-binary 10_000 cap.
	ChannelHealthState               *Gauge
	ChannelHealthFailureRate         *Gauge
	ChannelHealthConsecutiveFailures *Gauge
	ChannelHealthAlertsTotal         *Counter
	ChannelHealthRecoveriesTotal     *Counter

	// v3.5.0 EC-6 + EC-7 pricing + fulfilment seed metrics.
	// Cardinality budget per series:
	//   ec_supplier_cost_changes_total{tenant_id, source, direction}
	//     ~ tenants(10) * sources(4: 1688/taobao/aliexpress/
	//       pinduoduo) * directions(2) = 80 series.
	//   ec_pricing_decisions_total{tenant_id, outcome}
	//     ~ tenants(10) * outcomes(4: approved | approval_pending |
	//       guardrail_blocked | llm_failover) = 40 series.
	//   ec_pricing_change_pct
	//     ~ histogram (no labels) = 5 series (one per bucket plus
	//       sum + count surfaced as separate text format lines).
	//   ec_order_aggregator_normalisations_total{tenant_id, channel, status}
	//     ~ tenants(10) * channels(5) * statuses(3: ok/duplicate/
	//       failure) = 150 series.
	//   ec_dropship_orders_total{tenant_id, supplier, status}
	//     ~ tenants(10) * suppliers(2: 1688/aliexpress) *
	//       statuses(5: placed/approval_pending/rolled_back/
	//       fallback_used/no_fallback) = 100 series.
	//   ec_fx_rate_age_seconds
	//     ~ gauge (no labels) = 1 series.
	// Total ~ 376 additive series for v3.5.0; well under the
	// per-binary 10_000 cap.
	SupplierCostChangesTotal           *Counter
	PricingDecisionsTotal              *Counter
	PricingChangePctHistogram          *Histogram
	OrderAggregatorNormalisationsTotal *Counter
	DropshipOrdersTotal                *Counter
	FXRateAgeSeconds                   *Gauge

	// v3.6.0 EC-8 + EC-9 customer service + analytics metrics.
	// Cardinality budget per series:
	//   ec_enquiry_classifications_total{tenant_id, intent, sentiment, language}
	//     ~ tenants(10) * intents(8) * sentiments(4) * languages(4)
	//       = 1280 series upper bound; in practice tenants typically
	//       cluster on 1-2 languages so the live cap is ~64 series
	//       per tenant.
	//   ec_faq_responses_total{tenant_id, outcome}
	//     ~ tenants(10) * outcomes(4: auto_replied | suggested |
	//       escalated | no_match) = 40 series.
	//   ec_message_webhook_received_total{tenant_id, channel, status}
	//     ~ tenants(10) * channels(2: tiktok | facebook) *
	//       statuses(~10: replied/suggested/escalated/duplicate/
	//       hmac_failed/decode_failed/idempotency_error/read_failed/
	//       failed/pipeline_error) = 200 series.
	//   ec_gmv_rollup_request_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in the textual output).
	//   ec_sse_active_connections{tenant_id}
	//     ~ tenants(10) = 10 series.
	//   ec_sse_events_dispatched_total{tenant_id, event_type}
	//     ~ tenants(10) * event_types(10) = 100 series.
	// Total ~ 1631 additive series for v3.6.0; well under the
	// per-binary 10_000 cap.
	EnquiryClassificationsTotal *Counter
	FAQResponsesTotal           *Counter
	MessageWebhookReceivedTotal *Counter
	GMVRequestDurationSeconds   *Histogram
	SSEActiveConnections        *Gauge
	SSEEventsDispatchedTotal    *Counter

	// v3.7.0 EC-10 uiauto hardening metrics. Cardinality budget per
	// series:
	//   ec_uiauto_session_ops_total{tenant_id, channel, op}
	//     ~ tenants(10) * channels(4: rednote/tiktok/facebook/
	//       instagram) * ops(3: save/load/delete) = 120 series.
	//   ec_omniparser_inference_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	//   ec_omniparser_memory_pressure_pauses_total{tenant_id}
	//     ~ tenants(10) = 10 series.
	//   ec_omniparser_concurrent_inflight gauge
	//     ~ no labels = 1 series.
	//   ec_uiauto_rate_limit_drops_total{tenant_id, channel, reason}
	//     ~ tenants(10) * channels(4) * reasons(2: exceeded|drain)
	//       = 80 series.
	//   ec_captcha_detections_total{tenant_id, channel, signal}
	//     ~ tenants(10) * channels(3) * signals(4: body|status|dom|
	//       keyword) = 120 series upper-bound; live cap closer to
	//       30 because most tenants only see body+status hits.
	//   ec_captcha_resolution_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	// Total ~ 332 additive series upper-bound for v3.7.0; well
	// under the per-binary 10_000 cap.
	UIAutoSessionOpsTotal              *Counter
	OmniParserInferenceDurationSeconds *Histogram
	OmniParserMemoryPressurePauses     *Counter
	OmniParserConcurrentInflight       *Gauge
	UIAutoRateLimitDropsTotal          *Counter
	CAPTCHADetectionsTotal             *Counter
	CAPTCHAResolutionDurationSeconds   *Histogram

	// v3.8.0 EC-7 logistics + returns + EC-9-3 ROI metrics. Wired
	// in v3.8.1 QA via internal/observability/v380_metrics.go (the
	// production prom-client adapter for the 7 ports defined in
	// v3.8.0). Cardinality budget per series:
	//   ec_shipping_labels_generated_total{tenant_id, carrier, status}
	//     ~ tenants(10) * carriers(2: auspost|dhl) * statuses(4:
	//       generated|cached|sla_breach|all_failed) = 80 series.
	//   ec_shipping_label_cost_aud_cents{tenant_id, carrier} histogram
	//     ~ tenants(10) * carriers(2) = 20 series (10 buckets each).
	//   ec_status_propagation_duration_seconds{channel} histogram
	//     ~ channels(5) = 5 series.
	//   ec_returns_saga_state_transitions_total{tenant_id, state, auto_approved}
	//     ~ tenants(10) * states(7: requested|pending_approval|
	//       approved|labelled|refunded|completed|rolled_back) *
	//       auto_approved(2) = 140 series.
	//   ec_returns_refund_amount_aud_cents{tenant_id} histogram
	//     ~ tenants(10) = 10 series.
	//   ec_roi_query_duration_seconds{route} histogram
	//     ~ routes(3: heatmap|dead_stock|by_channel) = 3 series.
	//   ec_workflow_replay_assertions_total{workflow_name, outcome}
	//     ~ workflows(3: order_aggregator|dropship_saga|
	//       returns_saga) * outcomes(3: pass|non_determinism|fixture_load_failed) = 9 series.
	// Total ~ 267 additive series for v3.8.0/v3.8.1; well under
	// the per-binary 10_000 cap.
	ShippingLabelsGeneratedTotal     *Counter
	ShippingLabelCostAUDCents        *Histogram
	StatusPropagationDurationSeconds *Histogram
	ReturnsSagaStateTransitionsTotal *Counter
	ReturnsRefundAmountAUDCents      *Histogram
	ROIQueryDurationSeconds          *Histogram
	WorkflowReplayAssertionsTotal    *Counter

	// v3.9.1 Existing #10 + EC-9-4 + EC-9-5 + EC-4-4 metrics.
	// Cardinality budget per series:
	//   ec_onboarding_wizard_steps_completed_total{tenant_id, step}
	//     ~ tenants(10) * steps(4) = 40 series. Plan estimate ~50.
	//   ec_onboarding_wizard_completion_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	//   ec_channel_content_query_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	//   ec_operator_alerts_total{tenant_id, alert_type, status}
	//     ~ tenants(10) * alert_types(8) * statuses(4: pending|
	//       acknowledged|resolved|expired) = 320 series upper-bound;
	//       plan estimate ~150 series with realistic alert_type
	//       distribution + sweeper expirations.
	//   ec_operator_alerts_resolution_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	//   ec_stub_channel_calls_total{tenant_id, channel, op}
	//     ~ tenants(10) * channels(2: instagram|pinterest) *
	//       ops(3: publish|update_order_status|create_listing)
	//       = 60 series upper-bound; plan estimate ~40 once
	//       production stubs swap in v4.1.x.
	// Total ~ 250 additive series for v3.9.1; well under the
	// per-binary 10_000 cap.
	OnboardingWizardStepsCompleted     *Counter
	OnboardingWizardCompletionDuration *Histogram
	ChannelContentQueryDuration        *Histogram
	OperatorAlertsTotal                *Counter
	OperatorAlertsResolutionDuration   *Histogram
	StubChannelCallsTotal              *Counter

	// v3.9.0 EC-6-4 + EC-6-5 + EC-5-2 + EC-5-4 + EC-5-5 + carry-forward
	// metrics. Cardinality budget per series:
	//   ec_competitor_prices_observed_total{tenant_id, channel, undercut}
	//     ~ tenants(10) * channels(4) * undercut(2) = 80 series.
	//   ec_margin_dashboard_request_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	//   ec_content_calendar_entries_total{tenant_id, channel, status}
	//     ~ tenants(10) * channels(5) * statuses(5) = 250 series upper-bound;
	//       plan estimate ~150 series with realistic status distribution.
	//   ec_content_calendar_publishing_duration_seconds histogram
	//     ~ tenants(10) * channels(5) = 50 series (10 buckets each).
	//   ec_hashtag_caption_generations_total{tenant_id, channel, outcome}
	//     ~ tenants(10) * channels(4) * outcomes(2: llm|rule) = 80 series.
	//   ec_content_ema_score{tenant_id, channel, content_type}
	//     ~ tenants(10) * channels(4) * content_types(2) = 80 series upper-bound;
	//       plan estimate ~60 series (most tenants only use 1-2 content types).
	//   ec_content_ema_updates_total{tenant_id, channel}
	//     ~ tenants(10) * channels(5) = 50 series.
	//   ec_channel_status_updates_total{tenant_id, channel, outcome}
	//     ~ tenants(10) * channels(4) * outcomes(2: ok|failed) = 80 series.
	// Total ~ 520-650 additive series for v3.9.0; well under the
	// per-binary 10_000 cap. Plan estimate ~520 series.
	CompetitorPricesObservedTotal     *Counter
	MarginDashboardRequestDuration    *Histogram
	ContentCalendarEntriesTotal       *Counter
	ContentCalendarPublishingDuration *Histogram
	HashtagCaptionGenerationsTotal    *Counter
	ContentEMAScore                   *Gauge
	ContentEMAUpdatesTotal            *Counter
	ChannelStatusUpdatesTotal         *Counter

	// v4.2.0 payment foundation metrics. Cardinality budget per series:
	//   ec_payment_charges_total{tenant_id, provider, status}
	//     ~ tenants(10) * providers(3: stripe|alipay|wechat) *
	//       statuses(4: succeeded|failed|declined|pending) = 120 series.
	//   ec_payment_charge_duration_seconds{provider} histogram
	//     ~ providers(3) = 3 series (10 buckets each).
	//   ec_payment_refunds_total{tenant_id, provider, status}
	//     ~ tenants(10) * providers(3) * statuses(3: succeeded|failed|
	//       pending) = 90 series.
	// Total ~ 213 additive series for v4.2.0; well under the
	// per-binary 10_000 cap.
	PaymentChargesTotal   *Counter
	PaymentChargeDuration *Histogram
	PaymentRefundsTotal   *Counter

	// v4.7.0 MADRL coordination + tenant dashboard metrics.
	// Cardinality budget per series:
	//   ec_coord_resolutions_total{tenant_id, resolution_type}
	//     ~ tenants(10) * types(2: weighted_priority|constraint_override)
	//       = 20 series.
	//   ec_coord_reward_signals_total{tenant_id, agent_id}
	//     ~ tenants(10) * agents(3: pricing|fulfilment|content)
	//       = 30 series.
	//   ec_tenant_dashboard_request_duration_seconds histogram
	//     ~ no labels = 1 series (10 buckets in textual output).
	// Total ~ 51 additive series for v4.7.0; well under the
	// per-binary 10_000 cap.
	CoordResolutionsTotal          *Counter
	CoordRewardSignalsTotal        *Counter
	TenantDashboardRequestDuration *Histogram

	// v4.9.0 compliance + residency metrics. Cardinality budget:
	//   ec_compliance_deletions_total{tenant_id}  ~ tenants(10) = 10 series.
	//   ec_compliance_exports_total{tenant_id}    ~ tenants(10) = 10 series.
	//   ec_residency_violations_total{tenant_id, from_region, to_region}
	//     ~ tenants(10) * regions(4) * regions(4) = 160 upper-bound;
	//       in practice ~20 series (violations are rare).
	// Total ~ 40 additive series for v4.9.0; well under the
	// per-binary 10_000 cap.
	ComplianceDeletionsTotal *Counter
	ComplianceExportsTotal   *Counter
	ResidencyViolationsTotal *Counter

	// v4.11.0 Agentrace deep-integration metrics. Populated by
	// AgentraceAdapter during each evomap emission cycle.
	// See RegisterAgentraceMetrics for cardinality budget (~92 series).
	AgentraceSessionDuration  *Histogram
	AgentraceToolCallsTotal   *Counter
	AgentraceCostUSDTotal     *Counter
	AgentraceBottlenecksTotal *Counter
	AgentraceParallelismRatio *Gauge

	// v4.13.0 MiniMax quota-aware adapter metrics.
	// See RegisterMinimaxMetrics for cardinality budget (~38 series).
	MinimaxRequestsTotal        *Counter
	MinimaxRequestDuration      *Histogram
	MinimaxActiveKey            *Gauge
	MinimaxKeyCooldownRemaining *Gauge
	MinimaxFailoverEventsTotal  *Counter

	// v4.14.0 uiauto-vs-Playwright comparison harness metrics.
	// See RegisterComparisonMetrics for cardinality budget (~25 series).
	ComparisonAccuracy           *Gauge
	ComparisonSpeedMs            *Gauge
	ComparisonAgreementRate      *Gauge
	ComparisonScenarioDurationMs *Gauge
	ComparisonScenarioPassRate   *Gauge

	// v4.17.0 mem0 memory-layer client metrics.
	// Cardinality budget:
	//   ec_mem0_requests_total{op, status}
	//     ~ ops(3: store/search/delete) * statuses(4: ok/error/
	//       circuit_open/disabled) = 12 series.
	//   ec_mem0_request_duration_seconds{op} ~ 3 series + buckets.
	// Total ~ 20 additive series for v4.17.0.
	Mem0Requests *Counter
	Mem0Duration *Histogram

	// v5.5.0 Postgres connection pool metrics. Cardinality budget:
	//   ec_pg_pool_open_connections gauge ~ 1 series.
	//   ec_pg_pool_idle_connections gauge ~ 1 series.
	//   ec_pg_pool_wait_total counter ~ 1 series.
	//   ec_pg_pool_wait_duration_seconds histogram ~ 1 series + buckets.
	// Total ~ 4 additive series for v5.5.0 (+ ~10 bucket lines).
	PGPoolOpenConnections *Gauge
	PGPoolIdleConnections *Gauge
	PGPoolWaitTotal       *Counter
	PGPoolWaitDuration    *Histogram

	// v6.2.0 Story 3 memwatch v3 / generalized resilience metrics.
	// Cardinality budget per series:
	//   ec_workerpool_active{pool}            ~ pools(8) = 8 series.
	//   ec_workerpool_rejected_total{pool}    ~ pools(8) = 8 series.
	//   ec_breaker_open_total{name}           ~ breakers(8: mem0|minimax|stripe|
	//                                          alipay|wechat|auspost|dhl|misc) = 8 series.
	//   ec_breaker_half_open_total{name}      ~ breakers(8) = 8 series.
	//   ec_coord_conflicts_total{tenant_id, agent_a, agent_b, resolution}
	//     ~ tenants(10) * agent pairs(6) * resolutions(2) = 120 series.
	// Total ~ 152 additive series for v6.2.0; well under the
	// per-binary 10_000 cap.
	WorkerpoolActive     *Gauge
	WorkerpoolRejected   *Counter
	BreakerOpenTotal     *Counter
	BreakerHalfOpenTotal *Counter
	CoordConflictsTotal  *Counter
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
	r.ChannelHealthState = newGauge(r, "ec_channel_health_state", "v3.4.1 EC-4-5 channel health monitor state by tenant + channel (0=healthy, 1=degraded, 2=unhealthy).")
	r.ChannelHealthFailureRate = newGauge(r, "ec_channel_health_failure_rate", "v3.4.1 EC-4-5 channel health monitor sliding-window failure rate by tenant + channel.")
	r.ChannelHealthConsecutiveFailures = newGauge(r, "ec_channel_health_consecutive_failures", "v3.4.1 EC-4-5 channel health monitor consecutive-failure counter by tenant + channel.")
	r.ChannelHealthAlertsTotal = newCounter(r, "ec_channel_health_alerts_total", "v3.4.1 EC-4-5 channel health monitor transitions into degraded/unhealthy by tenant + channel + state.")
	r.ChannelHealthRecoveriesTotal = newCounter(r, "ec_channel_health_recoveries_total", "v3.4.1 EC-4-5 channel health monitor transitions back to healthy by tenant + channel.")
	r.SupplierCostChangesTotal = newCounter(r, "ec_supplier_cost_changes_total", "v3.5.0 EC-6-1 supplier cost monitor change detections by tenant + source + direction.")
	r.PricingDecisionsTotal = newCounter(r, "ec_pricing_decisions_total", "v3.5.0 EC-6-3 dynamic pricing agent decisions by tenant + outcome (approved|approval_pending|guardrail_blocked|llm_failover).")
	r.PricingChangePctHistogram = newHistogram(r, "ec_pricing_change_pct", "v3.5.0 EC-6-3 dynamic pricing agent absolute change percentage histogram.", defaultPctBuckets)
	r.OrderAggregatorNormalisationsTotal = newCounter(r, "ec_order_aggregator_normalisations_total", "v3.5.0 EC-7-1 multi-channel order aggregator normalisations by tenant + channel + status.")
	r.DropshipOrdersTotal = newCounter(r, "ec_dropship_orders_total", "v3.5.0 EC-7-2 drop-ship supplier orders by tenant + supplier + status.")
	r.FXRateAgeSeconds = newGauge(r, "ec_fx_rate_age_seconds", "v3.5.0 EC-6-2 AUD/CNY FX rate age (seconds since last refresh); ErrFXRateStale fires above 86400.")
	r.EnquiryClassificationsTotal = newCounter(r, "ec_enquiry_classifications_total", "v3.6.0 EC-8-1 enquiry classifier results by tenant + intent + sentiment + language.")
	r.FAQResponsesTotal = newCounter(r, "ec_faq_responses_total", "v3.6.0 EC-8-2 FAQ responder outcomes by tenant + outcome (auto_replied|suggested|escalated|no_match).")
	r.MessageWebhookReceivedTotal = newCounter(r, "ec_message_webhook_received_total", "v3.6.0 EC-8-3 inbound message webhook deliveries by tenant + channel + status.")
	r.GMVRequestDurationSeconds = newHistogram(r, "ec_gmv_rollup_request_duration_seconds", "v3.6.0 EC-9-1 GMV analytics request duration histogram.", defaultDurationBuckets)
	r.SSEActiveConnections = newGauge(r, "ec_sse_active_connections", "v3.6.0 EC-9-2 active SSE agent-activity connections by tenant.")
	r.SSEEventsDispatchedTotal = newCounter(r, "ec_sse_events_dispatched_total", "v3.6.0 EC-9-2 SSE events dispatched by tenant + event_type.")
	r.UIAutoSessionOpsTotal = newCounter(r, "ec_uiauto_session_ops_total", "v3.7.0 EC-10-1 uiauto session manager operations by tenant + channel + op (save|load|delete).")
	r.OmniParserInferenceDurationSeconds = newHistogram(r, "ec_omniparser_inference_duration_seconds", "v3.7.0 EC-10-2 OmniParser VLM inference duration histogram.", defaultDurationBuckets)
	r.OmniParserMemoryPressurePauses = newCounter(r, "ec_omniparser_memory_pressure_pauses_total", "v3.7.0 EC-10-2 memory-pressure pause events by tenant.")
	r.OmniParserConcurrentInflight = newGauge(r, "ec_omniparser_concurrent_inflight", "v3.7.0 EC-10-2 concurrent in-flight OmniParser inference requests.")
	r.UIAutoRateLimitDropsTotal = newCounter(r, "ec_uiauto_rate_limit_drops_total", "v3.7.0 EC-10-3 uiauto stealth rate-limit drops by tenant + channel + reason (exceeded|drain).")
	r.CAPTCHADetectionsTotal = newCounter(r, "ec_captcha_detections_total", "v3.7.0 EC-10-4 CAPTCHA detections by tenant + channel + signal (body|status|dom|keyword).")
	r.CAPTCHAResolutionDurationSeconds = newHistogram(r, "ec_captcha_resolution_duration_seconds", "v3.7.0 EC-10-4 CAPTCHA operator-resolution latency histogram.", defaultCAPTCHAResolutionBuckets)
	r.ShippingLabelsGeneratedTotal = newCounter(r, "ec_shipping_labels_generated_total", "v3.8.0 EC-7-3 shipping labels generated by tenant + carrier + status (generated|cached|sla_breach|all_failed).")
	r.ShippingLabelCostAUDCents = newHistogram(r, "ec_shipping_label_cost_aud_cents", "v3.8.0 EC-7-3 shipping label cost histogram (AUD cents) by tenant + carrier.", defaultShippingCostBuckets)
	r.StatusPropagationDurationSeconds = newHistogram(r, "ec_status_propagation_duration_seconds", "v3.8.0 EC-7-4 status propagation per-channel duration histogram.", defaultDurationBuckets)
	r.ReturnsSagaStateTransitionsTotal = newCounter(r, "ec_returns_saga_state_transitions_total", "v3.8.0 EC-7-5 returns saga state transitions by tenant + state + auto_approved.")
	r.ReturnsRefundAmountAUDCents = newHistogram(r, "ec_returns_refund_amount_aud_cents", "v3.8.0 EC-7-5 refund amount histogram (AUD cents) by tenant.", defaultRefundCentsBuckets)
	r.ROIQueryDurationSeconds = newHistogram(r, "ec_roi_query_duration_seconds", "v3.8.0 EC-9-3 ROI query duration histogram by route (heatmap|dead_stock|by_channel).", defaultDurationBuckets)
	r.WorkflowReplayAssertionsTotal = newCounter(r, "ec_workflow_replay_assertions_total", "v3.8.0 Existing #5 self-testing replay harness assertions by workflow_name + outcome (pass|non_determinism|fixture_load_failed).")
	r.CompetitorPricesObservedTotal = newCounter(r, "ec_competitor_prices_observed_total", "v3.9.0 EC-6-4 competitor price observations by tenant_id + channel + undercut (true|false).")
	r.MarginDashboardRequestDuration = newHistogram(r, "ec_margin_dashboard_request_duration_seconds", "v3.9.0 EC-6-5 margin dashboard request duration histogram.", defaultDurationBuckets)
	r.ContentCalendarEntriesTotal = newCounter(r, "ec_content_calendar_entries_total", "v3.9.0 EC-5-2 content calendar entry lifecycle transitions by tenant + channel + status.")
	r.ContentCalendarPublishingDuration = newHistogram(r, "ec_content_calendar_publishing_duration_seconds", "v3.9.0 EC-5-2 content calendar publishing duration histogram by tenant + channel.", defaultDurationBuckets)
	r.HashtagCaptionGenerationsTotal = newCounter(r, "ec_hashtag_caption_generations_total", "v3.9.0 EC-5-4 hashtag/caption generations by tenant + channel + outcome (llm|rule).")
	r.ContentEMAScore = newGauge(r, "ec_content_ema_score", "v3.9.0 EC-5-5 content performance EMA score by tenant + channel + content_type.")
	r.ContentEMAUpdatesTotal = newCounter(r, "ec_content_ema_updates_total", "v3.9.0 EC-5-5 content performance EMA updates by tenant + channel.")
	r.ChannelStatusUpdatesTotal = newCounter(r, "ec_channel_status_updates_total", "v3.9.0 carry-forward closure -- ChannelStatusUpdater outcomes by tenant + channel + outcome (ok|failed).")
	r.OnboardingWizardStepsCompleted = newCounter(r, "ec_onboarding_wizard_steps_completed_total", "v3.9.1 Existing #10 onboarding wizard step completions by tenant + step (1..4).")
	r.OnboardingWizardCompletionDuration = newHistogram(r, "ec_onboarding_wizard_completion_duration_seconds", "v3.9.1 Existing #10 onboarding wizard end-to-end completion duration histogram.", defaultDurationBuckets)
	r.ChannelContentQueryDuration = newHistogram(r, "ec_channel_content_query_duration_seconds", "v3.9.1 EC-9-4 channel content analytics request duration histogram.", defaultDurationBuckets)
	r.OperatorAlertsTotal = newCounter(r, "ec_operator_alerts_total", "v3.9.1 EC-9-5 operator alert lifecycle counter by tenant + alert_type + status (pending|acknowledged|resolved|expired).")
	r.OperatorAlertsResolutionDuration = newHistogram(r, "ec_operator_alerts_resolution_duration_seconds", "v3.9.1 EC-9-5 operator alert resolution duration histogram.", defaultDurationBuckets)
	r.StubChannelCallsTotal = newCounter(r, "ec_stub_channel_calls_total", "v3.9.1 EC-4-4 IG/Pinterest stub adapter calls by tenant + channel + op (publish|update_order_status|create_listing).")
	r.PaymentChargesTotal = newCounter(r, "ec_payment_charges_total", "v4.2.0 payment charges by tenant_id + provider (stripe|alipay|wechat) + status (succeeded|failed|declined|pending).")
	r.PaymentChargeDuration = newHistogram(r, "ec_payment_charge_duration_seconds", "v4.2.0 payment charge duration histogram by provider.", defaultDurationBuckets)
	r.PaymentRefundsTotal = newCounter(r, "ec_payment_refunds_total", "v4.2.0 payment refunds by tenant_id + provider + status (succeeded|failed|pending).")
	r.CoordResolutionsTotal = newCounter(r, "ec_coord_resolutions_total", "v4.7.0 MADRL coordination resolutions by tenant_id + resolution_type (weighted_priority|constraint_override).")
	r.CoordRewardSignalsTotal = newCounter(r, "ec_coord_reward_signals_total", "v4.7.0 MADRL reward signals emitted by tenant_id + agent_id.")
	r.TenantDashboardRequestDuration = newHistogram(r, "ec_tenant_dashboard_request_duration_seconds", "v4.7.0 per-tenant dashboard request duration histogram.", defaultDurationBuckets)
	r.ComplianceDeletionsTotal = newCounter(r, "ec_compliance_deletions_total", "v4.9.0 GDPR/CCPA right-to-delete operations by tenant_id.")
	r.ComplianceExportsTotal = newCounter(r, "ec_compliance_exports_total", "v4.9.0 GDPR Article 15 data exports by tenant_id.")
	r.ResidencyViolationsTotal = newCounter(r, "ec_residency_violations_total", "v4.9.0 data residency violation attempts by tenant_id + from_region + to_region.")
	RegisterAgentraceMetrics(r)
	RegisterMinimaxMetrics(r)
	RegisterComparisonMetrics(r)
	registerMem0Metrics(r)
	registerPGPoolMetrics(r)
	registerV620ResilienceMetrics(r)
	return r
}

// registerV620ResilienceMetrics registers the v6.2.0 worker pool +
// circuit breaker metrics. Kept as a standalone helper so the
// NewRegistry surface stays under the Sentrux complex_fn ceiling.
func registerV620ResilienceMetrics(r *Registry) {
	r.WorkerpoolActive = newGauge(r, "ec_workerpool_active", "v6.2.0 active workers per bounded pool by pool name.")
	r.WorkerpoolRejected = newCounter(r, "ec_workerpool_rejected_total", "v6.2.0 worker pool submissions rejected (saturated|closed) by pool name.")
	r.BreakerOpenTotal = newCounter(r, "ec_breaker_open_total", "v6.2.0 generalized circuit breaker transitions into the open state by name.")
	r.BreakerHalfOpenTotal = newCounter(r, "ec_breaker_half_open_total", "v6.2.0 generalized circuit breaker transitions into the half-open state by name.")
	r.CoordConflictsTotal = newCounter(r, "ec_coord_conflicts_total", "v6.2.0 CF-16 MADRL coordination conflicts by tenant_id + agent_a + agent_b + resolution.")
}

var defaultDurationBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

// defaultScoreBuckets is used by the supplier-score distribution
// histogram. Buckets cover the [0, 1] supplier-score range with
// 0.1-step granularity so reviewers can see the score distribution
// without label cardinality.
var defaultScoreBuckets = []float64{0.1, 0.2, 0.3, 0.4, 0.5, 0.6, 0.7, 0.8, 0.9, 1.0}

// defaultPctBuckets is used by the v3.5.0 EC-6-3 pricing change
// histogram. Buckets cover the [0, 0.5] absolute-change-pct range
// with finer granularity below 0.15 (the operator-approval
// threshold) so the dashboard can render the approval-gate cliff.
var defaultPctBuckets = []float64{0.01, 0.025, 0.05, 0.1, 0.15, 0.2, 0.3, 0.4, 0.5}

// defaultCAPTCHAResolutionBuckets is used by the v3.7.0 EC-10-4
// CAPTCHA operator-resolution latency histogram. Buckets cover
// the [10s, 1h] range -- the SolveBudget upper bound -- so the
// operator dashboard can spot SLO breaches before the budget fires.
var defaultCAPTCHAResolutionBuckets = []float64{10, 30, 60, 120, 300, 600, 1200, 1800, 2400, 3600}

// defaultShippingCostBuckets is used by the v3.8.0 EC-7-3 shipping
// label cost histogram. Buckets cover the [$5, $200] AUD range
// (in cents) which spans typical AusPost domestic + DHL
// international shipping. Bounded so an unbounded high-value label
// does not blow the cardinality budget.
var defaultShippingCostBuckets = []float64{500, 1000, 2000, 3000, 5000, 7500, 10000, 15000, 20000, 30000}

// defaultRefundCentsBuckets is used by the v3.8.0 EC-7-5 refund
// amount histogram. Buckets cover the [A$5, A$1000] range so the
// dashboard can pivot the auto-approve gate (A$50 = 5000 cents)
// against the long tail of high-value returns.
var defaultRefundCentsBuckets = []float64{500, 1000, 2500, 5000, 7500, 10000, 25000, 50000, 75000, 100000}

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
		r.ChannelHealthState.write(&sb)
		r.ChannelHealthFailureRate.write(&sb)
		r.ChannelHealthConsecutiveFailures.write(&sb)
		r.ChannelHealthAlertsTotal.write(&sb)
		r.ChannelHealthRecoveriesTotal.write(&sb)
		r.SupplierCostChangesTotal.write(&sb)
		r.PricingDecisionsTotal.write(&sb)
		r.PricingChangePctHistogram.write(&sb)
		r.OrderAggregatorNormalisationsTotal.write(&sb)
		r.DropshipOrdersTotal.write(&sb)
		r.FXRateAgeSeconds.write(&sb)
		r.EnquiryClassificationsTotal.write(&sb)
		r.FAQResponsesTotal.write(&sb)
		r.MessageWebhookReceivedTotal.write(&sb)
		r.GMVRequestDurationSeconds.write(&sb)
		r.SSEActiveConnections.write(&sb)
		r.SSEEventsDispatchedTotal.write(&sb)
		r.UIAutoSessionOpsTotal.write(&sb)
		r.OmniParserInferenceDurationSeconds.write(&sb)
		r.OmniParserMemoryPressurePauses.write(&sb)
		r.OmniParserConcurrentInflight.write(&sb)
		r.UIAutoRateLimitDropsTotal.write(&sb)
		r.CAPTCHADetectionsTotal.write(&sb)
		r.CAPTCHAResolutionDurationSeconds.write(&sb)
		r.ShippingLabelsGeneratedTotal.write(&sb)
		r.ShippingLabelCostAUDCents.write(&sb)
		r.StatusPropagationDurationSeconds.write(&sb)
		r.ReturnsSagaStateTransitionsTotal.write(&sb)
		r.ReturnsRefundAmountAUDCents.write(&sb)
		r.ROIQueryDurationSeconds.write(&sb)
		r.WorkflowReplayAssertionsTotal.write(&sb)
		r.CompetitorPricesObservedTotal.write(&sb)
		r.MarginDashboardRequestDuration.write(&sb)
		r.ContentCalendarEntriesTotal.write(&sb)
		r.ContentCalendarPublishingDuration.write(&sb)
		r.HashtagCaptionGenerationsTotal.write(&sb)
		r.ContentEMAScore.write(&sb)
		r.ContentEMAUpdatesTotal.write(&sb)
		r.ChannelStatusUpdatesTotal.write(&sb)
		r.OnboardingWizardStepsCompleted.write(&sb)
		r.OnboardingWizardCompletionDuration.write(&sb)
		r.ChannelContentQueryDuration.write(&sb)
		r.OperatorAlertsTotal.write(&sb)
		r.OperatorAlertsResolutionDuration.write(&sb)
		r.StubChannelCallsTotal.write(&sb)
		r.PaymentChargesTotal.write(&sb)
		r.PaymentChargeDuration.write(&sb)
		r.PaymentRefundsTotal.write(&sb)
		r.CoordResolutionsTotal.write(&sb)
		r.CoordRewardSignalsTotal.write(&sb)
		r.TenantDashboardRequestDuration.write(&sb)
		r.ComplianceDeletionsTotal.write(&sb)
		r.ComplianceExportsTotal.write(&sb)
		r.ResidencyViolationsTotal.write(&sb)
		r.AgentraceSessionDuration.write(&sb)
		r.AgentraceToolCallsTotal.write(&sb)
		r.AgentraceCostUSDTotal.write(&sb)
		r.AgentraceBottlenecksTotal.write(&sb)
		r.AgentraceParallelismRatio.write(&sb)
		r.MinimaxRequestsTotal.write(&sb)
		r.MinimaxRequestDuration.write(&sb)
		r.MinimaxActiveKey.write(&sb)
		r.MinimaxKeyCooldownRemaining.write(&sb)
		r.MinimaxFailoverEventsTotal.write(&sb)
		r.ComparisonAccuracy.write(&sb)
		r.ComparisonSpeedMs.write(&sb)
		r.ComparisonAgreementRate.write(&sb)
		r.ComparisonScenarioDurationMs.write(&sb)
		r.ComparisonScenarioPassRate.write(&sb)
		r.Mem0Requests.write(&sb)
		r.Mem0Duration.write(&sb)
		r.PGPoolOpenConnections.write(&sb)
		r.PGPoolIdleConnections.write(&sb)
		r.PGPoolWaitTotal.write(&sb)
		r.PGPoolWaitDuration.write(&sb)
		r.WorkerpoolActive.write(&sb)
		r.WorkerpoolRejected.write(&sb)
		r.BreakerOpenTotal.write(&sb)
		r.BreakerHalfOpenTotal.write(&sb)
		r.CoordConflictsTotal.write(&sb)
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
