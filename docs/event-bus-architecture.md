# Event Bus Architecture

## Overview

The agentic-ecommerce stack uses **Redis Streams** as its internal event bus,
providing at-least-once delivery semantics with consumer groups. This replaces
the v1.0.0 in-memory event dispatch and enables decoupled, async communication
between services.

## Why Redis Streams

| Requirement | Redis Streams answer |
|---|---|
| At-least-once delivery | Consumer groups with ACK |
| Persistent event log | AOF-backed, survives restarts |
| Dead-letter queue | Pending Entry List (PEL) + claim after timeout |
| Multiple consumers | Consumer group fan-out |
| Backpressure | `XREADGROUP BLOCK` with configurable count |
| Operational simplicity | Single Redis already deployed for cache |

## Stream Topology

```
ec.product.events      ← product.created, product.updated, product.deleted
ec.order.events        ← order.placed, order.paid, order.shipped
ec.sync.events         ← sync.started, sync.completed, sync.failed
ec.agent.events        ← agent.run.started, agent.run.completed
ec.compliance.events   ← compliance.checked, compliance.passed, compliance.failed
ec.sync.deadletter     ← failed events after max retry
```

## Consumer Groups

Each service registers a consumer group on the streams it consumes:

| Service | Group | Streams |
|---|---|---|
| mc-api | `ec-api-group` | all streams (readiness probe) |
| wc-sync | `ec-sync-group` | `ec.product.events`, `ec.order.events` |
| agent-worker | `ec-agent-group` | `ec.product.events`, `ec.compliance.events` |
| content-worker | `ec-content-group` | `ec.agent.events` |

## Delivery Semantics

1. **Producers** use `XADD` with auto-generated IDs (`*`).
2. **Consumers** use `XREADGROUP GROUP <group> <consumer> BLOCK <ms> COUNT <n>`.
3. After processing, consumers issue `XACK`.
4. Unacknowledged messages past `ECOMMERCE_EVENTBUS_CLAIM_TIMEOUT` (default 60s)
   are claimed by other consumers via `XAUTOCLAIM`.
5. Messages exceeding max delivery attempts move to the dead-letter stream.

## Event Envelope

All events share a common envelope:

```json
{
  "id": "01JXY...",
  "type": "product.created",
  "source": "mc-api",
  "tenant_id": "default",
  "timestamp": "2026-05-07T12:00:00Z",
  "data": { "product_id": 42 }
}
```

## Configuration

Environment variables controlling the event bus:

| Variable | Default | Description |
|---|---|---|
| `ECOMMERCE_EVENTBUS_DRIVER` | `redis` | Event bus backend (`redis` or `noop`) |
| `ECOMMERCE_EVENTBUS_REDIS_ADDR` | same as `ECOMMERCE_REDIS_ADDR` | Redis address for event bus |
| `ECOMMERCE_EVENTBUS_REDIS_DB` | `0` | Redis DB index for streams |
| `ECOMMERCE_EVENTBUS_CONSUMER_GROUP` | `ec-api-group` | Consumer group name for this service |
| `ECOMMERCE_EVENTBUS_STREAMS` | `ec.product.events,...` | Comma-separated stream names |
| `ECOMMERCE_EVENTBUS_CHANNEL_SYNC` | `ec.sync.events` | Legacy sync channel name |
| `ECOMMERCE_EVENTBUS_CHANNEL_DLQ` | `ec.sync.deadletter` | Dead-letter stream |

## Redis Configuration

The `configs/redis-streams.conf` file is mounted into the Redis container and
sets:

- `maxmemory-policy noeviction` — prevents silent stream data loss
- `appendonly yes` with `appendfsync everysec` — durability
- Stream node tuning for compact radix trees

## Health Check

The `/readyz` endpoint includes an `eventbus` probe that verifies:

1. TCP connectivity to the event bus Redis instance
2. Successful PING/PONG exchange

This probe is **required** (non-optional) when `ECOMMERCE_EVENTBUS_DRIVER=redis`.

## Future Work (v1.2.0+)

- Temporal server consumes events to trigger durable workflows
- n8n webhook bridge subscribes to streams for outbound webhooks
- Stream trimming policy (`MAXLEN ~10000`) for bounded memory growth
