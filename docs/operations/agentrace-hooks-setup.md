# Agentrace Hooks Setup

> v4.11.0 — Production wiring for Cursor hooks → Agentrace → EvoMap → EvoLoop pipeline.

## Overview

Agentrace captures agent session events (tool calls, costs, bottlenecks, parallelism)
from Cursor IDE hooks and feeds them into the EvoMap/EvoLoop observability pipeline.

```
Cursor Hooks → cursor-tools agentrace append-event
  → ~/.agentrace/events.jsonl
  → Agentrace reducer → insights
  → EvoMap adapter → KPI capsule → Prometheus → Grafana
```

## Cursor hooks.json Configuration

Add the following hooks to your project or global Cursor hooks configuration.

### Shell Execution Tracking

```json
{
  "hooks": [
    {
      "event": "beforeShellExecution",
      "command": "cursor-tools agentrace append-event --type shell_start --tool \"$TOOL_NAME\"",
      "description": "Record shell command start for Agentrace telemetry"
    },
    {
      "event": "afterShellExecution",
      "command": "cursor-tools agentrace append-event --type shell_end --tool \"$TOOL_NAME\" --outcome \"$EXIT_CODE\"",
      "description": "Record shell command end for Agentrace telemetry"
    }
  ]
}
```

### Session Lifecycle Events

```json
{
  "hooks": [
    {
      "event": "onSessionStart",
      "command": "cursor-tools agentrace append-event --type session_start --session-id \"$SESSION_ID\"",
      "description": "Mark agent session start for duration tracking"
    },
    {
      "event": "onSessionEnd",
      "command": "cursor-tools agentrace append-event --type session_end --session-id \"$SESSION_ID\"",
      "description": "Mark agent session end for duration tracking"
    }
  ]
}
```

## Pipeline Architecture

### Stage 1: Event Collection (Cursor Hooks)

Cursor hooks fire on shell execution and session lifecycle. Each hook invokes
`cursor-tools agentrace append-event` which appends a single NDJSON line to
`~/.agentrace/events.jsonl`.

### Stage 2: Reduction (Agentrace Reducer)

The Agentrace reducer (`cursor-tools agentrace reduce`) processes raw events
into insights: session boundaries, tool usage aggregates, cost summaries,
bottleneck detections, and parallelism scores. State is persisted in
`~/.agentrace/state.sqlite`.

### Stage 3: Insight Serving

Insights are served via the Agentrace loopback SSE server at
`127.0.0.1:8100/api/insights` (started by `cursor-tools agentrace serve`).

### Stage 4: EvoMap Adapter (This Repo)

`internal/evomap/agentrace_adapter.go` reads insights from the loopback HTTP
endpoint (with JSONL file fallback) and transforms them into EvoMap KPI fields:

| Agentrace Source | EvoMap KPI Field |
|---|---|
| Session boundaries | `agentrace_session_duration_seconds` |
| Tool usage insights | `agentrace_tool_call_count` |
| Cost tracker | `agentrace_cost_usd` |
| Bottleneck detector | `agentrace_bottleneck_count` |
| Parallelism scorer | `agentrace_parallelism_efficiency` |

### Stage 5: Prometheus Metrics

`internal/metrics/agentrace_metrics.go` registers five Prometheus metrics
populated by the adapter during each emission cycle:

- `ec_agentrace_session_duration_seconds` (histogram)
- `ec_agentrace_tool_calls_total{tool_name, outcome}` (counter)
- `ec_agentrace_cost_usd_total{session_id}` (counter)
- `ec_agentrace_bottlenecks_total{severity}` (counter)
- `ec_agentrace_parallelism_ratio` (gauge)

### Stage 6: Grafana Dashboard

`monitoring/grafana/agentrace-insights.json` (uid: `agentrace-insights-v4110`)
provides six panels: session timeline, tool call heatmap, cost per session,
bottleneck timeline, parallelism efficiency gauge, and error rate per tool.

## Compatibility Notes

- The hooks bridge from v325-4 (Tarsa era) is fully compatible after the
  Agentrace rename. The `cursor-tools tarsa` alias remains functional for
  one minor release per ADR-026.
- Graceful degradation: if Agentrace is not running (server down, no JSONL
  file), the adapter emits `agentrace_available=false` and all KPI fields
  default to zero. No errors propagate to the EvoMap pipeline.

## Verification

```bash
# Verify Agentrace server is running
curl -s http://127.0.0.1:8100/api/insights | jq .

# Verify events are being collected
wc -l ~/.agentrace/events.jsonl

# Verify Prometheus metrics are exposed
curl -s http://localhost:9090/api/v1/query?query=ec_agentrace_tool_calls_total | jq .
```
