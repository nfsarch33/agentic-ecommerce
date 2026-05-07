# Agent Schedule Operations

v1.6.0 adds the operational wiring for Temporal-backed recurring agent runs. This
document intentionally covers compose config, Temporal CLI inspection, and
monitoring only; schedule creation and workflow business logic stay with the
backend agent workflow owner.

## Configuration

Schedules are disabled by default so local compose cannot duplicate manual agent
runs or the existing worker loop:

```bash
ECOMMERCE_AGENT_SCHEDULES_ENABLED=false
ECOMMERCE_AGENT_SCHEDULES_DEFAULT_INTERVAL=15m
ECOMMERCE_AGENT_SCHEDULES_MAX_CONCURRENT_RUNS=1
ECOMMERCE_AGENT_SCHEDULES_TASK_QUEUE=ec-workflows
```

`agent-worker` exposes these values on `/metrics` for dashboard and alert
wiring. `temporal-worker` logs the same values at startup and polls
`ECOMMERCE_TEMPORAL_TASK_QUEUE`, which defaults to `ec-workflows`.

## Compose Validation

Validate the schedule-related profiles before starting workers:

```bash
make compose-agent-schedules-config
make compose-temporal-config
make compose-workers-config
```

The target validates both `docker-compose.yml` and `docker-compose.dev.yml` with
the `workers`, `temporal`, and `temporal-worker` profiles enabled.

## Temporal CLI Inspection

Start Temporal and list registered schedules:

```bash
make temporal-up
make agent-schedules-list
```

Run the combined local smoke when you want compose config validation plus a
Temporal schedule-list probe:

```bash
make agent-schedules-smoke
```

An empty list is expected until the backend schedule owner registers concrete
agent schedules.

## Monitoring

The worker metrics reserve these schedule series:

```text
agentic_ecommerce_agent_schedules_enabled
agentic_ecommerce_agent_schedule_default_interval_seconds
agentic_ecommerce_agent_schedule_max_concurrent_runs
agentic_ecommerce_agent_schedule_config_info{task_queue="ec-workflows"}
agentic_ecommerce_agent_scheduled_runs_total{task_queue="ec-workflows",status="failed"}
```

`AgenticEcommerceScheduledAgentFailuresHigh` fires when scheduled agent failures
are exposed by the worker metrics. Keep `make monitoring-validate` in the local
gate before opening or merging infra changes.
