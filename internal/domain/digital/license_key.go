package digital

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"errors"
	"fmt"
	"strings"
)

// licenseKeyParts is the number of dash-separated groups in a generated
// licence key. The structure is INDEX-INDEX-INDEX-INDEX-CHECKSUM where
// the first four groups encode tenant + product + customer entropy and
// the last group is an HMAC-SHA256 truncated checksum.
const licenseKeyParts = 5

// licenseKeyGroupSize is the number of characters in each non-checksum
// group. Five characters from a base32 alphabet gives ~25 bits of
// entropy per group, ~100 bits across the four groups -- more than
// enough to survive bulk guessing without burning column width.
const licenseKeyGroupSize = 5

// licenseKeyChecksumSize is the number of characters in the trailing
// HMAC checksum group. Eight base32 characters carry 40 bits of
// signal, the smallest size that keeps tampering costs prohibitive
// without making the key uncomfortable to type.
const licenseKeyChecksumSize = 8

// licenseKeyAlphabet is RFC 4648 base32 with padding stripped: clear
// letters and digits, no ambiguity between O/0 or I/1.
var licenseKeyAlphabet = base32.StdEncoding.WithPadding(base32.NoPadding)

// LicenseKeyGenerator is the typed seam every caller MUST go through to
// mint a new key. Production builds inject a real HMAC-SHA256 generator
// keyed by an env-sourced secret; tests inject a deterministic generator
// so test ledger files stay golden.
//
// The generator does NOT see persistent storage; it only owns the
// secret material. Callers are responsible for persisting the returned
// key on a Licence aggregate.
type LicenseKeyGenerator interface {
	// Generate returns a new licence key string. seed is application-
	// supplied entropy (e.g. the licence UUID) so the resulting key is
	// stable for that licence and reproducible by tests.
	Generate(tenantID string, seed []byte) (string, error)

	// Validate verifies an HMAC checksum against tenantID and the
	// payload portion of the key. Returns ErrInvalidLicense on any
	// tampering or shape error.
	Validate(tenantID string, key string) error
}

// HMACLicenseKeyGenerator is the production-grade LicenseKeyGenerator.
// It uses crypto/hmac + crypto/sha256 from the stdlib only and
// constant-time comparison via crypto/subtle.ConstantTimeCompare to
// prevent timing oracles when the operator supplies a tampered key.
type HMACLicenseKeyGenerator struct {
	secret []byte
	rand   func([]byte) (int, error)
}

// HMACLicenseKeyGeneratorOption tunes the HMAC generator. Tests may
// inject a deterministic random source via WithRandSource so generated
// keys stay golden.
type HMACLicenseKeyGeneratorOption func(*HMACLicenseKeyGenerator)

// WithRandSource overrides the random source used for the entropy
// portion of generated keys. Production callers should leave this
// alone; tests use it to inject a counter.
func WithRandSource(src func([]byte) (int, error)) HMACLicenseKeyGeneratorOption {
	return func(g *HMACLicenseKeyGenerator) {
		if src != nil {
			g.rand = src
		}
	}
}

// NewHMACLicenseKeyGenerator constructs an HMAC-SHA256 backed generator.
// The secret MUST be at least 32 bytes; shorter values are rejected so
// operators do not accidentally deploy a weakened gate.
func NewHMACLicenseKeyGenerator(secret []byte, opts ...HMACLicenseKeyGeneratorOption) (*HMACLicenseKeyGenerator, error) {
	if len(secret) < 32 {
		return nil, errors.New("license key generator secret must be at least 32 bytes")
	}
	g := &HMACLicenseKeyGenerator{secret: append([]byte(nil), secret...)}
	g.rand = cryptoRand
	for _, opt := range opts {
		opt(g)
	}
	return g, nil
}

// Generate produces a fresh key tied to tenantID. seed is folded into
// the rand source via the test-only WithRandSource option so fixtures
// stay golden; in production it is ignored. The cryptographic
// uniqueness comes from the random payload entropy plus the HMAC over
// (tenantID, payload), so a key minted for tenant A cannot be replayed
// against tenant B even if the secret leaks the same payload bytes.
func (g *HMACLicenseKeyGenerator) Generate(tenantID string, seed []byte) (string, error) {
	if strings.TrimSpace(tenantID) == "" {
		return "", ErrTenantRequired
	}
	_ = seed // Reserved for callers that want to mix application entropy
	// in tests; the production rand source is OS-backed and ignores it.
	const groups = licenseKeyParts - 1
	rawBytesNeeded := groups * licenseKeyGroupSize
	rawBytes := make([]byte, rawBytesNeeded)
	if _, err := g.rand(rawBytes); err != nil {
		return "", fmt.Errorf("license key entropy: %w", err)
	}
	encoded := licenseKeyAlphabet.EncodeToString(rawBytes)
	if len(encoded) < groups*licenseKeyGroupSize {
		return "", errors.New("license key alphabet underflow")
	}
	encoded = encoded[:groups*licenseKeyGroupSize]
	parts := make([]string, 0, licenseKeyParts)
	for i := 0; i < groups; i++ {
		start := i * licenseKeyGroupSize
		parts = append(parts, encoded[start:start+licenseKeyGroupSize])
	}
	payload := strings.Join(parts, "-")
	checksum := g.checksum(tenantID, payload)
	parts = append(parts, checksum)
	return strings.Join(parts, "-"), nil
}

// Validate verifies an HMAC checksum against tenantID and the payload
// portion of the key. Returns ErrInvalidLicense on any tampering or
// shape error.
func (g *HMACLicenseKeyGenerator) Validate(tenantID string, key string) error {
	tenantID = strings.TrimSpace(tenantID)
	if tenantID == "" {
		return ErrTenantRequired
	}
	parts := strings.Split(strings.TrimSpace(key), "-")
	if len(parts) != licenseKeyParts {
		return ErrInvalidLicenseKey
	}
	for i := 0; i < licenseKeyParts-1; i++ {
		if len(parts[i]) != licenseKeyGroupSize {
			return ErrInvalidLicenseKey
		}
	}
	if len(parts[licenseKeyParts-1]) != licenseKeyChecksumSize {
		return ErrInvalidLicenseKey
	}
	payload := strings.Join(parts[:licenseKeyParts-1], "-")
	got := []byte(parts[licenseKeyParts-1])
	want := []byte(g.checksum(tenantID, payload))
	if subtle.ConstantTimeCompare(got, want) != 1 {
		return ErrInvalidLicense
	}
	return nil
}

// checksum returns a base32 truncated HMAC-SHA256 over (tenantID,
// payload). Tenant id and payload are joined by an explicit unit
// separator (0x1f) so a tenant string ending in payload-prefix bytes
// cannot collide with a tenant prefix.
func (g *HMACLicenseKeyGenerator) checksum(tenantID, payload string) string {
	mac := hmac.New(sha256.New, g.secret)
	mac.Write([]byte(tenantID))
	mac.Write([]byte{0x1f})
	mac.Write([]byte(payload))
	digest := mac.Sum(nil)
	encoded := licenseKeyAlphabet.EncodeToString(digest)
	if len(encoded) < licenseKeyChecksumSize {
		return encoded
	}
	return encoded[:licenseKeyChecksumSize]
}

// cryptoRand is the production entropy source. It is var-not-const so
// tests can swap it via WithRandSource without exposing the
// implementation choice in the public API.
var cryptoRand = func(b []byte) (int, error) {
	return cryptoRandReader(b)
}
