// Package session ships the v3.7.0 EC-10-1 session manager that
// persists per-tenant per-channel uiauto session blobs (browser
// cookies, localStorage, sessionStorage) using OS-native secret
// stores with file-based AES-256-GCM as the cross-platform fallback.
//
// Design notes:
//
//   - SessionManager is a small interface with platform-specific
//     implementations selected at construction time. macOS uses the
//     `/usr/bin/security` CLI (existing pattern in runx; cited in
//     the no-shell-leak rule). Linux + WSL + CI use a pure-Go AES-
//     256-GCM file store keyed off the EC_SESSION_MASTER_KEY env var
//     (32 hex bytes minimum).
//
//   - Session blobs are encrypted-at-rest even when stored in the OS
//     keychain (defense in depth -- the keychain itself is encrypted
//     but a stolen unlocked Mac shouldn't leak browser cookies in
//     plaintext to an attacker holding `security` access).
//
//   - Tenant isolation: every operation takes tenant_id; the
//     keychain account / file path is derived from `<tenant_id>-
//     <channel>` so tenant A cannot read tenant B's cookies even if
//     the keychain ACL is misconfigured.
//
//   - Expiry: sessions older than 30 days are rejected on Load and
//     return ErrSessionExpired. The recorded_at timestamp is part of
//     the encrypted blob (so an attacker who tampers with the file
//     metadata cannot rewind the clock).
//
// Decomposition discipline (HARD GATE: complex_fn must NOT increase
// from 4):
//
//   - Save splits into serialize -> encrypt -> write helpers.
//   - Load splits into read -> decrypt -> deserialize helpers.
//
// Per-function cyclomatic stays under 6 (well below the v3.6.1
// sentrux ceiling).
package session

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"
)

// MaxSessionAge bounds how long a stored session blob can live before
// LoadSession rejects it with ErrSessionExpired. 30 days is the
// operator-tunable default; the keychain entry is NOT auto-deleted so
// the operator can re-bootstrap an expired session through the live
// browser flow.
const MaxSessionAge = 30 * 24 * time.Hour

// MasterKeyEnvVar is the env var the file-based store reads to
// derive the AES-256-GCM key. Format: 32-byte hex (64 hex chars).
// Documented in CLAUDE / AGENTS.md as part of the v3.7.0
// composition-root contract.
const MasterKeyEnvVar = "EC_SESSION_MASTER_KEY"

// MasterKeyBytes is the required length for the decoded master key.
const MasterKeyBytes = 32

// EC-10-1 typed sentinels.
var (
	// ErrSessionNotFound is returned by LoadSession / DeleteSession
	// when no entry exists for the (tenant_id, channel) pair.
	ErrSessionNotFound = errors.New("session: not found")

	// ErrSessionExpired is returned by LoadSession when the entry's
	// recorded_at is older than MaxSessionAge. Keys are NOT auto-
	// deleted so the operator can choose to re-bootstrap.
	ErrSessionExpired = errors.New("session: expired (>30 days)")

	// ErrKeychainUnavailable is returned by the macOS implementation
	// when /usr/bin/security is missing or returns an unexpected
	// status (operator should fall back to the file store).
	ErrKeychainUnavailable = errors.New("session: keychain unavailable")

	// ErrInvalidMasterKey is returned by NewFileManager when
	// EC_SESSION_MASTER_KEY is missing, malformed, or too short.
	ErrInvalidMasterKey = errors.New("session: invalid master key (need 32 hex bytes)")

	// ErrInvalidTenantID is returned when the tenant_id is empty or
	// contains the channel separator (would break the keychain
	// account derivation).
	ErrInvalidTenantID = errors.New("session: invalid tenant_id")

	// ErrInvalidChannel is returned when the channel is empty or
	// contains the channel separator.
	ErrInvalidChannel = errors.New("session: invalid channel")

	// ErrSessionTampered is returned by LoadSession when the AES-
	// GCM auth tag fails to verify -- i.e. the ciphertext or the
	// blob has been modified.
	ErrSessionTampered = errors.New("session: tamper detected (AES-GCM auth fail)")
)

// channelKeySeparator separates tenant_id from channel in the
// keychain account / file path. Hyphen is illegal in tenant_id by
// the validation rules so we use it as the safe separator.
const channelKeySeparator = ":"

// ChannelKey identifies one stored session.
type ChannelKey struct {
	TenantID string
	Channel  string
}

// SessionBlob is the JSON payload persisted in encrypted form. The
// fields mirror the standard Playwright/chromedp storage state shape
// so the omniparser-bridge can round-trip it without translation.
type SessionBlob struct {
	Cookies        []map[string]any  `json:"cookies"`
	LocalStorage   map[string]string `json:"localStorage,omitempty"`
	SessionStorage map[string]string `json:"sessionStorage,omitempty"`
	UserAgent      string            `json:"ua,omitempty"`
	RecordedAt     time.Time         `json:"recorded_at"`
}

// SessionManager is the small port that EC-10-2 / EC-10-4 / EC-10-5
// depend on. Implementations are platform-specific (macOS keychain
// or AES-GCM file store).
type SessionManager interface {
	SaveSession(ctx context.Context, tenantID, channel string, blob SessionBlob) error
	LoadSession(ctx context.Context, tenantID, channel string) (SessionBlob, error)
	DeleteSession(ctx context.Context, tenantID, channel string) error
	ListSessions(ctx context.Context, tenantID string) ([]ChannelKey, error)
}

// validateKey checks tenant_id + channel for the separator-safety
// rules. Both are required; neither may contain the separator.
func validateKey(tenantID, channel string) error {
	if strings.TrimSpace(tenantID) == "" {
		return ErrInvalidTenantID
	}
	if strings.Contains(tenantID, channelKeySeparator) {
		return fmt.Errorf("%w: must not contain %q", ErrInvalidTenantID, channelKeySeparator)
	}
	if strings.TrimSpace(channel) == "" {
		return ErrInvalidChannel
	}
	if strings.Contains(channel, channelKeySeparator) {
		return fmt.Errorf("%w: must not contain %q", ErrInvalidChannel, channelKeySeparator)
	}
	return nil
}

// composeAccount combines tenant_id and channel into the keychain
// account / file basename.
func composeAccount(tenantID, channel string) string {
	return tenantID + channelKeySeparator + channel
}

// decomposeAccount is the inverse of composeAccount; ListSessions
// uses it to project keychain entries back to ChannelKeys.
func decomposeAccount(account string) (tenantID, channel string, ok bool) {
	parts := strings.SplitN(account, channelKeySeparator, 2)
	if len(parts) != 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// serialize marshals a SessionBlob into the canonical JSON form. The
// recorded_at field is overwritten with `now` if zero-valued so
// callers do not need to remember to set it.
func serialize(blob SessionBlob, now time.Time) ([]byte, error) {
	if blob.RecordedAt.IsZero() {
		blob.RecordedAt = now.UTC()
	}
	out, err := json.Marshal(blob)
	if err != nil {
		return nil, fmt.Errorf("session: marshal blob: %w", err)
	}
	return out, nil
}

// deserialize parses a previously-serialized SessionBlob.
func deserialize(payload []byte) (SessionBlob, error) {
	var blob SessionBlob
	if err := json.Unmarshal(payload, &blob); err != nil {
		return SessionBlob{}, fmt.Errorf("session: unmarshal blob: %w", err)
	}
	return blob, nil
}

// resolveMasterKey decodes the EC_SESSION_MASTER_KEY env var into the
// 32-byte AES-256 key. Returns ErrInvalidMasterKey on any malformed
// input so callers can branch on the typed sentinel.
func resolveMasterKey(hexKey string) ([]byte, error) {
	if hexKey == "" {
		return nil, fmt.Errorf("%w: env %s unset", ErrInvalidMasterKey, MasterKeyEnvVar)
	}
	raw, err := hex.DecodeString(hexKey)
	if err != nil {
		return nil, fmt.Errorf("%w: hex decode: %v", ErrInvalidMasterKey, err)
	}
	if len(raw) != MasterKeyBytes {
		return nil, fmt.Errorf("%w: got %d bytes, want %d", ErrInvalidMasterKey, len(raw), MasterKeyBytes)
	}
	return raw, nil
}

// encrypt wraps plaintext in AES-256-GCM with a fresh random nonce.
// The on-disk format is `nonce || ciphertext` so the GCM auth tag
// covers the entire ciphertext (any tamper -> ErrSessionTampered on
// decrypt).
func encrypt(masterKey, plaintext []byte) ([]byte, error) {
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("session: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("session: gcm: %w", err)
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, fmt.Errorf("session: nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plaintext, nil), nil
}

// decrypt reverses encrypt. Tamper -> ErrSessionTampered.
func decrypt(masterKey, payload []byte) ([]byte, error) {
	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("session: cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("session: gcm: %w", err)
	}
	nonceSize := gcm.NonceSize()
	if len(payload) < nonceSize {
		return nil, fmt.Errorf("%w: payload < nonce size", ErrSessionTampered)
	}
	nonce, ciphertext := payload[:nonceSize], payload[nonceSize:]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrSessionTampered, err)
	}
	return plaintext, nil
}

// checkExpiry returns ErrSessionExpired if the blob is older than
// MaxSessionAge.
func checkExpiry(recordedAt time.Time, now time.Time, maxAge time.Duration) error {
	if recordedAt.IsZero() {
		return nil
	}
	age := now.Sub(recordedAt)
	if age > maxAge {
		return fmt.Errorf("%w: age=%s", ErrSessionExpired, age)
	}
	return nil
}
