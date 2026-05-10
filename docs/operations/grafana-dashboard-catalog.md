# Grafana Dashboard Catalog

> Last verified: 2026-05-11

Complete catalog of Grafana dashboards shipped with the Agentic E-Commerce stack.

## Top-Level Dashboards (`monitoring/grafana/`)

| File | UID | Title | Panels | Version | Metrics Used |
|------|-----|-------|--------|---------|-------------|
| `agentrace-insights.json` | `agentrace-insights-v4110` | Agentrace Insights | 6 | v4.11.0 | `ec_agentrace_session_duration_seconds`, `ec_agentrace_tool_calls_total`, `ec_agentrace_cost_usd_total`, `ec_agentrace_bottlenecks_total`, `ec_agentrace_parallelism_ratio` |
| `channel-health.json` | — | Channel Health Monitor | 6 | v3.4.1 | `ec_channel_health_state`, `ec_channel_health_failure_rate`, `ec_channel_health_consecutive_failures`, `ec_channel_health_alerts_total`, `ec_channel_health_recoveries_total`, `ec_channel_router_dispatches_total` |
| `enrichment-pipeline.json` | — | Enrichment Pipeline | 6 | v3.2.0 | `ec_enrichment_runs_total`, `ec_enrichment_duration_seconds`, `ec_enrichment_quality_score`, `ec_image_processing_total`, `ec_trend_ingest_records_total`, `ec_seo_keyword_injects_total` |
| `minimax-quota.json` | — | MiniMax Quota Monitor | 5 | v4.13.0 | `ec_minimax_requests_total`, `ec_minimax_failover_events_total`, `ec_minimax_key_cooldown_remaining_seconds`, `ec_minimax_request_duration_seconds`, `ec_minimax_active_key` |
| `uiauto-comparison.json` | — | uiauto vs Playwright Comparison | 5 | v4.14.0 | `ec_uiauto_comparison_agreement_rate`, `ec_uiauto_comparison_speed_ms`, `ec_uiauto_comparison_scenario_pass_rate`, `ec_uiauto_comparison_scenario_duration_ms` |

## v2.10.0 Dashboards (`monitoring/grafana/dashboards/v210/`)

| File | Title | Panels | Metrics Used |
|------|-------|--------|-------------|
| `ec-overview.json` | EC Overview | 4 | `ec_http_requests_total`, `ec_http_duration_seconds` |
| `ec-resilience.json` | EC Resilience | 4 | `ec_oom_alarms_total`, `ec_goroutine_count`, `ec_heap_bytes`, `ec_workflow_runs_total` |
| `ec-tenant.json` | EC Tenant View | 3 | `ec_http_requests_total`, `ec_http_duration_seconds` (tenant-scoped) |
| `ec-workerpools.json` | EC Worker Pools | 2 | `ec_workerpool_queued`, `ec_workerpool_saturation_total` |

## v4.2.0 Dashboards (`monitoring/grafana/dashboards/v420/`)

| File | Title | Panels | Metrics Used |
|------|-------|--------|-------------|
| `ec-channel-status.json` | Channel Status | 6 | `ec_channel_status_updates_total`, `ec_channel_health_state`, `ec_returns_saga_state_transitions_total`, `ec_shipping_labels_generated_total`, `ec_operator_alerts_total`, `ec_payment_charges_total` |
| `ec-content-ema.json` | Content EMA Scores | 4 | `ec_content_ema_score`, `ec_content_ema_updates_total`, `ec_content_calendar_entries_total`, `ec_hashtag_caption_generations_total` |
| `ec-pricing-decisions.json` | Pricing Decisions | 4 | `ec_pricing_decisions_total`, `ec_pricing_change_pct`, `ec_supplier_cost_changes_total`, `ec_fx_rate_age_seconds` |

## Templates (`monitoring/grafana/dashboards/`)

| File | Title | Notes |
|------|-------|-------|
| `tenant-template.json` | Per-Tenant Template | Replace `__TENANT_ID__` placeholder with actual tenant ID |
| `agentic-ecommerce-overview.json` | Agentic Ecommerce Overview | Legacy dashboard (pre-v2.10.0 metric names updated in v5.9.0) |

## Total: 14 dashboard files

## Stale Metrics Fixed in v5.9.0

| Dashboard | Old Metric | New Metric |
|-----------|-----------|------------|
| `tenant-template.json` | `mc_api_http_requests_total` | `ec_http_requests_total` |
| `tenant-template.json` | `mc_api_http_request_duration_seconds` | `ec_http_duration_seconds` |
| `agentic-ecommerce-overview.json` | `agentic_ecommerce_http_requests_total` | `ec_http_requests_total` |
| `agentic-ecommerce-overview.json` | `agentic_ecommerce_http_request_duration_seconds` | `ec_http_duration_seconds` |
| `agentic-ecommerce-overview.json` | `agentic_ecommerce_agent_worker_runs_total` | `ec_workflow_runs_total` |

## Metric Registry Cross-Reference

All `ec_*` metrics are registered in `internal/metrics/metrics.go`. The registry currently defines
~100 distinct metric names across counters, gauges, and histograms, covering versions v2.10.0
through v5.5.0. See the `NewRegistry` function for the complete list.
