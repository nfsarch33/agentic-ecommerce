# API Versioning Policy

> **Status**: stable as of v2.9.0.

The Agentic Ecommerce HTTP surface ships with two named versions:

| Version    | Path prefix | Status   | Spec file                       | Stability                                                            |
|------------|-------------|----------|---------------------------------|----------------------------------------------------------------------|
| `v1`       | `/api/v1/`  | stable   | `api/openapi.yaml`              | No breaking changes through host **v3.x**. Additive only.            |
| `v2`       | `/api/v2/`  | preview  | `api/openapi-v2-preview.yaml`   | Subject to change without notice. Opt-in only.                       |

## Stability guarantees

### v1 endpoints (stable)

- Existing endpoints will not change their request/response schemas
  in incompatible ways.
- New optional fields may be added to responses; clients that do
  strict-shape validation should ignore unknown fields.
- New endpoints may be added under `/api/v1/...`.
- Endpoints marked `deprecated` in the OpenAPI spec will continue to
  function for at least one major release after the deprecation
  notice ships in `CHANGELOG.md`.
- Default verb / status code semantics will not change.

### v2 endpoints (preview)

- Schemas, paths, and verbs may change between minor releases.
- Every v2 response carries `X-API-Version: 2-preview` and
  `X-API-Deprecation: preview; semantics may change without notice`
  so client logging can flag drift early.
- v2 endpoints will be promoted to v1 (or merged into the v1 surface
  with new fields) once the schema settles.
- A v2 endpoint may be deleted between minor releases if the
  underlying design proves unsound. v1 endpoints will never be
  deleted without a major release.

## Negotiation

Clients pick a version through one of two mechanisms:

### Path-based (preferred)

Hit the explicit prefix:

```
GET /api/v1/marketplace/plugins/foo
GET /api/v2/marketplace/plugins/foo/install
```

### Accept-header negotiation

Useful when a client wants the same path to upgrade automatically
when a v2 endpoint becomes available:

```
GET /api/v1/products
Accept: application/vnd.ec.v2+json
```

If a v2 handler is mounted for that path, the response uses v2
semantics; otherwise the server falls back to v1. Path-based opt-in
always wins over the Accept header, so a typo'd Accept value cannot
silently downgrade an explicit v2 caller.

The supported media types are:

- `application/vnd.ec.v1+json` -- explicit v1 opt-in (no behavioural
  difference vs the default; useful for pinning).
- `application/vnd.ec.v2+json` -- preview opt-in.

## Response headers

Every API response carries:

- `X-API-Version: 1` or `X-API-Version: 2-preview` -- the version the
  handler used. Clients should log this for auditing.
- `X-API-Deprecation: preview; semantics may change without notice`
  -- only set on v2 preview responses. Clients that surface this
  header in dashboards will catch drift between SDK upgrades.

## Versioning the SDK

The Plugin SDK at `pkg/marketplace/sdk` follows the same v1 stability
contract. SDK consumers can rely on the exported symbols not changing
through host v3.x. New helpers may be added; none will be removed.

## Deprecation timeline

When an endpoint or SDK symbol is marked deprecated, the host emits:

1. `CHANGELOG.md` entry under "Deprecated" in the next minor release
2. `Deprecation` header on the affected v1 endpoint citing the
   replacement path
3. Removal allowed in the next major release after deprecation

For v2 preview endpoints, the deprecation header is implicit (the
`X-API-Deprecation` header is always present) so any v2 endpoint may
change in any minor release without a separate notice.

## Examples

### Pin a v1 client

```bash
curl -H "Accept: application/vnd.ec.v1+json" https://api.example.com/api/v1/products
# X-API-Version: 1
```

### Opt into a v2 preview endpoint

```bash
curl -H "Accept: application/vnd.ec.v2+json" \
  -X POST https://api.example.com/api/v2/marketplace/plugins/stripe-payments/install
# X-API-Version: 2-preview
# X-API-Deprecation: preview; semantics may change without notice
```

### Detect a downgrade in CI

```bash
RESP_VERSION=$(curl -sI -H "Accept: application/vnd.ec.v2+json" https://api.example.com/api/v2/x | awk '/X-API-Version/ {print $2}' | tr -d '\r')
if [ "$RESP_VERSION" != "2-preview" ]; then
  echo "expected v2 preview, got $RESP_VERSION" >&2
  exit 1
fi
```
