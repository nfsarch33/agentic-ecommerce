# EC v8 Pair 3 Research -- Shopee Adapter

**Date**: 2026-05-12
**Scope**: Shopee product-sync adapter behind the shared `marketplacesync.Connector` port.
**Branch**: `feat/v8-p03-shopee-adapter`

## Evidence Sources

- Shopee Open Platform v2 authorization guide, 2020-09 PDF from Shopee CDN:
  `https://cdngarenanow-a.akamaihd.net/shopee/seller/seller_cms/c575929f948611337e1249564c2b8ff6/%5BTW%5D%5BOpen%20API%5DAPI%20v1_v2%E6%8E%88%E6%AC%8A%E6%96%B9%E6%B3%95%20%282020_09%29_newnew.pdf`
- Shopee Open Platform integration notes, 2020-10 PDF from Shopee CDN:
  `https://cdngarenanow-a.akamaihd.net/shopee/seller/seller_cms/b17e7e1846b98c422e4404f223b9f65f/%5BTW%5D%5BOpen%20API%5DAPI%E4%B8%B2%E6%8E%A5%E8%AA%AA%E6%98%8E%E4%BA%8B%E9%A0%85%20%282020_10_21%29_newnew.pdf`
- Pair 1 marketplace sync core: `internal/marketplacesync`.
- Pair 2 Shopify adapter: `internal/adapter/shopify`.

## Official Shopee Findings

- Shopee v2 requests include `partner_id`, `timestamp`, and `sign` on the URL.
- Shop-scoped calls also include `access_token` and `shop_id`.
- The v2 API request signature base string is:
  `partner_id + api_path + timestamp + access_token + shop_id`.
- The request signature algorithm is HMAC-SHA256 with the partner key, hex encoded.
- Authorization redirects are region and partner specific, and live shop access requires
  partner credentials plus a seller authorization flow.

## Local Evidence Boundary

Shopee's current Open Platform product reference pages are dynamic and were not
available to this Codex tool surface as stable machine-readable pages. This MVP
therefore implements only a guarded, deterministic product-upsert request shape
against mocked HTTP servers:

- no live Shopee calls,
- no OAuth flow,
- no webhook ingestion,
- no batching or pagination,
- no committed credentials,
- shared engine owns retry, DLQ, replay, idempotency, and metrics.

The adapter intentionally leaves live sandbox validation and cassette hygiene to
Pair 3 QA after official partner-console evidence can be captured.

## Design Decision

Add a sibling adapter package, `internal/adapter/shopee`, that implements:

```text
marketplacesync.Connector
```

The client signs each HTTP request using pure signing helpers, sends a single
product upsert event to the configured v2 product endpoint, and returns
`marketplacesync.ApplyResult` from the mocked Shopee response. Provider errors
are surfaced as ordinary connector errors so the shared Pair 1 engine can retry
and DLQ them.

## TDD Plan

RED tests first:

- deterministic HMAC-SHA256 signature canonical form,
- constant-time signature verification,
- signed shop-scoped query parameters on outbound product requests,
- product payload mapping from `marketplacesync.ProductEvent`,
- existing Shopee item id reuse for update-style payloads,
- unsupported provider/entity/operation rejection,
- Shopee API errors returned to the shared sync engine.

## Carry-Forward to QA

- Capture official partner-console sandbox screenshots or approved docs for
  product add/update payload fields.
- Add cassette fixture scans for Shopee token, shop, and signature markers.
- Prove no-live-call behavior with blocked external hosts.
- Add retry-to-DLQ integration against the shared engine.
- Re-run branch-local Sentrux gate against the worktree path.
