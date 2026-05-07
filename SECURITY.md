# Security Policy

This repository is safe to publish only while it contains generic source,
tests, documentation, redacted examples, and placeholder configuration.
Do not commit live operational data.

Never commit:

- `.env` files or exported environment dumps
- WooCommerce consumer keys or consumer secrets
- MiniMax, OpenAI, GitHub, AWS, 1Password, JFrog, or other API tokens
- SSH keys, `.pem`, `.key`, `.crt`, or identity files
- Browser session profiles, cookies, or screenshots containing account data
- Private hostnames, fleet inventories, internal IPs, OCI IDs, or Tailscale node details
- Customer, candidate, proposal, or application data

## MiniMax policy

This backend must not call `api.minimaxi.com` directly from the MacBook.
MiniMax traffic runs through the fleet-side `minimax-openai-bridge`, with
key selection state managed through `runx minimax` and the approved
Tailscale/OCI nodes.

## Runtime security controls

Protected backend operations use short-lived JWT bearer tokens with RBAC roles
`admin`, `operator`, and `viewer`. Configure `ECOMMERCE_JWT_SECRET`,
`ECOMMERCE_ADMIN_USERNAME`, and `ECOMMERCE_ADMIN_PASSWORD` outside the
repository. `ECOMMERCE_API_TOKEN` is a legacy migration control only.

Required JWT/RBAC controls:

- Sign JWTs with a secret or KMS-backed key stored outside the repository.
- Validate issuer, audience, expiry, not-before, and token ID on every request.
- Keep refresh tokens server-side or revocable; never expose them to browser
  JavaScript.
- Enforce RBAC in backend middleware, not only in the frontend UI.
- Log authentication, authorisation, and mutation decisions with redacted
  identifiers and request IDs.

Required rate-limit controls:

- Apply per-IP and per-principal token buckets at the API edge.
- Use `ECOMMERCE_RATE_LIMIT_CAPACITY` and `ECOMMERCE_RATE_LIMIT_REFILL` for
  token-bucket tuning.
- Use Redis-backed counters for multi-instance deployments when Redis is
  configured.
- Emit structured logs and metrics for rejected requests.

## CORS and CSP

`ECOMMERCE_ALLOWED_ORIGIN` is a single-origin allowlist for browser requests.
Leave it blank for same-origin/server-to-server local testing; set it to the
exact storefront origin in shared or production environments. Do not use `*`
with authenticated API routes.

Deployments must set a Content Security Policy at the reverse proxy, load
balancer, CDN, or frontend platform. Start from a deny-by-default policy and
explicitly allow the storefront origin, backend origin, approved image/media
hosts, and the fleet bridge only when a BFF route requires it.

Example CSP baseline:

```text
default-src 'self'; base-uri 'self'; frame-ancestors 'none'; object-src 'none'; connect-src 'self' https://api.example.com; img-src 'self' data: https:; script-src 'self'; style-src 'self' 'unsafe-inline'
```

## Reporting

Report vulnerabilities privately through GitHub security advisories.
