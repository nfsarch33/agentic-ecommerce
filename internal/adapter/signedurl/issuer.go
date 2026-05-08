// Package signedurl implements port.DownloadTokenIssuer using HMAC-
// SHA256 signed URLs.
//
// Design notes:
//   - The URL shape is deliberately simple:
//     <base>?lid=<licence-uuid>&pid=<product-uuid>&tid=<tenant>&exp=<unix>&sig=<base64>
//     so callers can audit the URL with grep without opaque tokens.
//   - The signature is taken over a deterministic canonical form
//     (tenant + license + product + expiry + uses-allowed) so subtle
//     re-orderings cannot bypass the gate.
//   - Verification uses crypto/subtle.ConstantTimeCompare to prevent
//     timing oracles when an attacker tries to brute the signature.
//   - Tenant id is part of the signed payload so a leaked URL cannot
//     be replayed against another tenant even if both tenants live in
//     the same gateway process.
package signedurl

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nfsarch33/agentic-ecommerce/internal/domain/digital"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

// ErrSecretTooShort is returned when an issuer is built with a secret
// shorter than 32 bytes. Mirrors the licence-key generator policy.
var ErrSecretTooShort = errors.New("signed url issuer secret must be at least 32 bytes")

// ErrSignatureMissing is returned when the URL does not carry a sig
// query parameter.
var ErrSignatureMissing = errors.New("signed url is missing signature")

// HMACIssuer implements port.DownloadTokenIssuer with HMAC-SHA256.
type HMACIssuer struct {
	baseURL string
	secret  []byte
}

// Config configures an HMACIssuer.
type Config struct {
	// BaseURL is the public-facing prefix (e.g.
	// "https://cdn.example.com/api/v1/digital-downloads"). The issuer
	// appends query parameters but does not mutate path bytes.
	BaseURL string
	// Secret is the HMAC key. Must be at least 32 bytes. Production
	// callers source this from an env var; tests inject a fixed value.
	Secret []byte
}

// New builds an HMACIssuer.
func New(cfg Config) (*HMACIssuer, error) {
	if len(cfg.Secret) < 32 {
		return nil, ErrSecretTooShort
	}
	base := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	return &HMACIssuer{baseURL: base, secret: append([]byte(nil), cfg.Secret...)}, nil
}

// Issue mints a signed URL.
func (i *HMACIssuer) Issue(req port.IssueDownloadRequest) (port.IssueDownloadResponse, error) {
	if strings.TrimSpace(req.TenantID) == "" {
		return port.IssueDownloadResponse{}, digital.ErrTenantRequired
	}
	if req.LicenseID == uuid.Nil {
		return port.IssueDownloadResponse{}, errors.New("license id required")
	}
	if req.ProductID == uuid.Nil {
		return port.IssueDownloadResponse{}, errors.New("product id required")
	}
	if req.TTL <= 0 {
		return port.IssueDownloadResponse{}, errors.New("ttl must be positive")
	}
	if req.UsesAllowed <= 0 {
		req.UsesAllowed = 1
	}
	issuedAt := req.IssuedAt
	if issuedAt.IsZero() {
		issuedAt = time.Now().UTC()
	}
	issuedAt = issuedAt.UTC()
	expiresAt := issuedAt.Add(req.TTL)

	sig := i.sign(req.TenantID, req.LicenseID, req.ProductID, expiresAt, req.UsesAllowed)

	q := url.Values{}
	q.Set("tid", req.TenantID)
	q.Set("lid", req.LicenseID.String())
	q.Set("pid", req.ProductID.String())
	q.Set("exp", strconv.FormatInt(expiresAt.Unix(), 10))
	q.Set("uses", strconv.Itoa(req.UsesAllowed))
	q.Set("sig", sig)

	publicURL := i.baseURL
	if !strings.Contains(publicURL, "?") {
		publicURL += "?"
	} else {
		publicURL += "&"
	}
	publicURL += q.Encode()

	tok, err := digital.NewDownloadToken(digital.DownloadTokenInput{
		TenantID:    req.TenantID,
		LicenseID:   req.LicenseID,
		Signature:   sig,
		ExpiresAt:   expiresAt,
		UsesAllowed: req.UsesAllowed,
		Now:         issuedAt,
	})
	if err != nil {
		return port.IssueDownloadResponse{}, fmt.Errorf("download token: %w", err)
	}
	return port.IssueDownloadResponse{URL: publicURL, Token: tok}, nil
}

// Verify decodes and validates a previously-issued signed URL.
func (i *HMACIssuer) Verify(rawURL string, now time.Time) (port.DownloadClaims, error) {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return port.DownloadClaims{}, fmt.Errorf("parse url: %w", err)
	}
	q := parsed.Query()

	tenantID := q.Get("tid")
	if tenantID == "" {
		return port.DownloadClaims{}, digital.ErrTenantRequired
	}
	licenseID, err := uuid.Parse(q.Get("lid"))
	if err != nil {
		return port.DownloadClaims{}, fmt.Errorf("invalid lid: %w", err)
	}
	productID, err := uuid.Parse(q.Get("pid"))
	if err != nil {
		return port.DownloadClaims{}, fmt.Errorf("invalid pid: %w", err)
	}
	expUnix, err := strconv.ParseInt(q.Get("exp"), 10, 64)
	if err != nil {
		return port.DownloadClaims{}, fmt.Errorf("invalid exp: %w", err)
	}
	uses, err := strconv.Atoi(q.Get("uses"))
	if err != nil || uses <= 0 {
		return port.DownloadClaims{}, fmt.Errorf("invalid uses")
	}
	sig := q.Get("sig")
	if sig == "" {
		return port.DownloadClaims{}, ErrSignatureMissing
	}

	expiresAt := time.Unix(expUnix, 0).UTC()
	want := i.sign(tenantID, licenseID, productID, expiresAt, uses)
	if subtle.ConstantTimeCompare([]byte(sig), []byte(want)) != 1 {
		return port.DownloadClaims{}, digital.ErrInvalidLicense
	}
	if !now.UTC().Before(expiresAt) {
		return port.DownloadClaims{}, digital.ErrTokenExpired
	}
	return port.DownloadClaims{
		TenantID:  tenantID,
		LicenseID: licenseID,
		ProductID: productID,
		ExpiresAt: expiresAt,
	}, nil
}

// sign returns the canonical HMAC-SHA256 signature, base64-url encoded
// without padding so it survives query-string round-trips.
func (i *HMACIssuer) sign(tenantID string, licenseID, productID uuid.UUID, expiresAt time.Time, uses int) string {
	mac := hmac.New(sha256.New, i.secret)
	mac.Write([]byte(tenantID))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(licenseID.String()))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(productID.String()))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(strconv.FormatInt(expiresAt.Unix(), 10)))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(strconv.Itoa(uses)))
	digest := mac.Sum(nil)
	return base64.RawURLEncoding.EncodeToString(digest)
}
