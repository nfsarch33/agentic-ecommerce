# v9.0.0 Webhook Contracts

The backend supports inbound WooCommerce webhooks and outbound tenant-scoped event delivery for n8n or other approved automation receivers. n8n local operation is documented in `docs/n8n-local.md`; this file records the API and signing contract for release review.

## Event Envelope

Outbound deliveries use the shared event envelope:

```json
{
  "id": "01JXY000000000000000000000",
  "type": "order.placed",
  "source": "mc-api",
  "tenant_id": "default",
  "timestamp": "2026-05-08T00:00:00Z",
  "data": {
    "order_id": "order-demo-001"
  }
}
```

The `data` object is event-specific and must not include secrets. Use stable identifiers and fetch private details through authenticated backend APIs when needed.

## Outbound Registration API

| Route | Method | Purpose |
| --- | --- | --- |
| `/api/v1/webhooks` | `POST` | Register a tenant-scoped endpoint URL, subscribed event types, and one write-only signing secret. |
| `/api/v1/webhooks` | `GET` | List registrations without returning raw secrets. |
| `/api/v1/webhooks/{id}` | `DELETE` | Delete a registration for the current tenant scope. |
| `/api/v1/webhooks/{id}/test` | `POST` | Send a signed test event to the registered endpoint. |

Supported event types in v9.0.0:

- `product.created`
- `product.updated`
- `order.placed`
- `sync.completed`
- `agent.run.completed`
- `compliance.checked`

Registrations are protected by JWT/RBAC and tenant scope. The API returns a `secret_ref` or hash metadata, never the raw signing secret.

## Delivery Signing

Every outbound delivery includes HMAC metadata headers:

```text
X-Agentic-Event-ID: 01JXY000000000000000000000
X-Agentic-Event-Type: order.placed
X-Agentic-Tenant-ID: default
X-Agentic-Timestamp: 2026-05-08T00:00:00Z
X-Agentic-Signature: sha256=<hex-hmac>
```

Receivers should verify the timestamp freshness, reconstruct the signed payload from the raw request body, compare the signature in constant time, and reject replayed event IDs. n8n workflows should keep signing secrets in n8n credentials or environment variables, never in checked-in workflow JSON.

## n8n Workflow Templates

Checked-in templates live under `deploy/n8n/workflows/`:

- `product-approved-slack-notification.json`
- `order-placed-email-confirmation.json`

The templates are inactive by default, contain no `credentials` blocks, and use environment placeholder URLs for provider-facing HTTP Request nodes. Validate them before import:

```bash
make n8n-workflows-validate
make n8n-config
```

After import, copy the generated local n8n webhook URL into an outbound webhook registration through the backend API. Do not hard-code backend, Slack, SMTP, or provider URLs into workflow JSON.

## Inbound WooCommerce Webhooks

WooCommerce inbound routes remain separate from the outbound automation bridge:

- `POST /api/v1/webhooks/woocommerce/orders`
- `POST /api/v1/webhooks/woocommerce/products`

These routes validate WooCommerce signatures with the configured webhook secret, convert accepted payloads into backend domain events, and publish follow-up events through Redis Streams when configured.

## Security Review Checklist

- Keep published n8n and webhook test ports bound to loopback in local compose.
- Do not commit provider credentials, raw webhook secrets, exported n8n credentials, `.env`, `.env.compose`, `.tfvars`, account IDs, project IDs, private hostnames, or internal IPs.
- Run `runx shell-leak-scan --repo ecommerce` after editing docs, workflow JSON, or environment examples.
