# Flutter Admin App API Contract

> v4.8.0 -- Mobile-optimized JSON API surface for the Flutter admin app.
>
> Last verified: 2026-05-11

## Overview

This document describes the backend API endpoints designed for consumption by a
Flutter-based mobile admin application. All endpoints are optimised for mobile
payloads (compact JSON, pagination-aware) and require JWT authentication with
tenant scoping.

## Authentication

All endpoints require:
- `Authorization: Bearer <jwt>` header with valid short-lived JWT
- `X-Tenant-Id: <tenant_id>` header for tenant scoping

## Endpoints

### GET /api/v1/admin/summary

Compact dashboard KPIs for the mobile home screen.

**Response:**
```json
{
  "data": {
    "active_orders": 15,
    "gmv_today_aud_cents": 750000,
    "pending_alerts": 2,
    "channel_health": [
      {"channel": "tiktok", "status": "healthy"},
      {"channel": "instagram", "status": "degraded"}
    ]
  }
}
```

### GET /api/v1/admin/orders

Paginated order list with minimal fields for mobile display.

**Query Parameters:**
| Param | Type | Default | Description |
|-------|------|---------|-------------|
| page  | int  | 1       | Page number |
| limit | int  | 20      | Items per page (max 100) |

**Response:**
```json
{
  "data": [
    {
      "order_id": "ord-1",
      "status": "processing",
      "total_cents": 5000,
      "channel": "tiktok",
      "created_at": "2026-05-10T10:00:00Z"
    }
  ],
  "meta": {"page": 1, "limit": 20, "total": 45},
  "links": {
    "next": "/api/v1/admin/orders?page=2&limit=20",
    "prev": null
  }
}
```

### POST /api/v1/admin/alerts/{id}/action

Quick alert resolution from mobile push notifications.

**Query Parameters:**
| Param  | Type   | Description |
|--------|--------|-------------|
| action | string | One of: `approve`, `deny`, `snooze` |

**Response:**
```json
{
  "data": {
    "alert_id": "alert-42",
    "action": "approve",
    "status": "resolved"
  }
}
```

### GET /api/v1/admin/channels

Channel health summary for mobile widget display.

**Response:**
```json
{
  "data": [
    {"channel": "tiktok", "status": "healthy"},
    {"channel": "instagram", "status": "degraded"},
    {"channel": "facebook", "status": "healthy"}
  ]
}
```

## Response Envelope

All endpoints follow the standard envelope:
```json
{
  "data": "...",
  "meta": {"page": 1, "limit": 20, "total": 45},
  "links": {"next": "...", "prev": "..."}
}
```

`meta` and `links` are only present on paginated endpoints.

## Error Responses

```json
{"error": "handler: admin tenant_id missing"}
```

HTTP status codes: 400 (bad request), 401 (unauthorized), 404 (not found),
500 (server error), 503 (service unavailable).

## Flutter Integration Notes

1. **SDK generation**: Use `openapi-generator` with the Dart client to generate
   type-safe API bindings from `api/openapi.yaml`.
2. **Pagination**: The `links.next`/`links.prev` fields provide ready-to-use
   URLs for infinite scroll implementation.
3. **Real-time updates**: Combine with the existing SSE endpoint
   (`GET /api/v1/agent-activity/stream`) for live dashboard updates.
4. **Offline support**: Cache the `/admin/summary` response locally for
   offline-first UX; refresh on connectivity restore.
