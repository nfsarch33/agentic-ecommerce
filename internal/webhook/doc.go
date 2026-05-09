// Package webhook hosts the v3.3.0 EC-3-3 inbound webhook
// receivers for external storefronts (TikTok Shop today, Facebook
// Messenger + RedNote in v3.4 / v3.6). Every receiver follows the
// verify-then-parse contract: HMAC verification on the *raw* body
// before json.Decode, so a forged event never reaches the domain
// layer.
//
// Resilience pillar (v2.10 baseline):
//
//   - Each handler implements lifecycle.Closer.
//   - Workerpool is supplied by the composition root for
//     concurrent webhook processing (currently no fan-out; the
//     handler is request-scoped on the net/http goroutine).
//   - Errors typed and %w-wrapped via package sentinels.
//   - Tenant awareness: every persisted dedup row + emitted
//     OrderReceivedEvent carries TenantID.
//   - Idempotency: a tenant-scoped IdempotencyStore guards
//     duplicate-delivery so retried webhooks are no-ops.
//
// Cite skill: go-security-review (HMAC verify-then-parse, replay
// tolerance, constant-time compare via internal/adapter/social).
package webhook
