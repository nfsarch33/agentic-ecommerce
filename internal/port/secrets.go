package port

import (
	"context"
	"errors"
)

// TenantSecretStore is the v2.7.0 port for per-tenant credential
// retrieval. The marketplace + cloud-scale slice introduces tenant-
// scoped secrets (Stripe keys, license HMAC keys, registration HMAC
// keys, URL-signing HMAC keys) so each tenant can swap credentials
// independently. The v2.8.0 consolidation collapses the three
// previous adapter packages (awssecrets, gcpsecrets, stubsecrets)
// into a single backend-selectable Manager at
// internal/adapter/secrets, mirroring the internal/adapter/notification
// package layout (one package, multiple typed senders).
//
// Implementations MUST namespace lookups under
// "agentic-ecommerce/<tenantID>/<key>" and MUST return ErrSecretNotFound
// when the path is missing rather than empty string. Returned values
// are never logged or echoed back to the caller verbatim per the
// no-shell-leak rule.
type TenantSecretStore interface {
	// GetTenantSecret returns the value for the requested key
	// scoped to tenantID. tenantID and key must be non-empty.
	// Implementations MUST treat the value as opaque and never
	// log or trace it.
	GetTenantSecret(ctx context.Context, tenantID, key string) (string, error)
}

// Canonical secret keys exposed via TenantSecretStore. Adapters route
// these to the underlying provider path, e.g.
// "agentic-ecommerce/<tenant>/stripe-api-key" on AWS Secrets Manager.
const (
	SecretKeyStripeAPIKey         = "stripe-api-key"
	SecretKeyStripeWebhookSecret  = "stripe-webhook-secret"
	SecretKeyLicenseHMACKey       = "license-hmac"
	SecretKeyURLHMACKey           = "url-hmac"
	SecretKeyRegistrationHMACKey  = "registration-hmac"
	SecretKeyMarketplaceWebhook   = "marketplace-webhook"
	SecretKeyTenantOverrideAPIKey = "api-key"
)

// ErrSecretNotFound is returned by TenantSecretStore.GetTenantSecret
// when the requested tenant/key tuple does not exist.
var ErrSecretNotFound = errors.New("port: tenant secret not found")

// ErrSecretInvalidArgument is returned when tenantID or key is empty
// or contains characters outside [a-z0-9-_].
var ErrSecretInvalidArgument = errors.New("port: tenant secret invalid argument")
