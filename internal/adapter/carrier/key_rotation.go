// File scope: v4.6.0 -- Carrier webhook signing key rotation policy.
//
// Supports dual-key verification during rotation window: both
// current and previous signing keys are accepted. The previous
// key auto-expires after a configurable TTL (default 48h).
//
// Config env vars:
//
//	EC_AUSPOST_WEBHOOK_KEY_CURRENT, EC_AUSPOST_WEBHOOK_KEY_PREVIOUS
//	EC_DHL_WEBHOOK_KEY_CURRENT, EC_DHL_WEBHOOK_KEY_PREVIOUS
//	EC_CARRIER_KEY_ROTATION_TTL_HOURS (default 48)
package carrier

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Key rotation env vars.
const (
	EnvAusPostKeyCurrent  = "EC_AUSPOST_WEBHOOK_KEY_CURRENT"
	EnvAusPostKeyPrevious = "EC_AUSPOST_WEBHOOK_KEY_PREVIOUS"
	EnvDHLKeyCurrent      = "EC_DHL_WEBHOOK_KEY_CURRENT"
	EnvDHLKeyPrevious     = "EC_DHL_WEBHOOK_KEY_PREVIOUS"
	EnvKeyRotationTTL     = "EC_CARRIER_KEY_ROTATION_TTL_HOURS"
)

const DefaultKeyRotationTTL = 48 * time.Hour

var (
	ErrKeyRotationUnconfigured = errors.New("carrier: key rotation unconfigured")
	ErrSignatureRejected       = errors.New("carrier: webhook signature rejected")
	ErrPreviousKeyExpired      = errors.New("carrier: previous key expired")
)

// KeyRotationConfig wires a KeyRotator for a single carrier.
type KeyRotationConfig struct {
	CarrierName string
	CurrentKey  string
	PreviousKey string
	PreviousSet time.Time
	TTL         time.Duration
	Now         func() time.Time
}

// KeyRotator manages dual-key verification for a carrier.
type KeyRotator struct {
	mu          sync.RWMutex
	carrierName string
	currentKey  string
	previousKey string
	previousSet time.Time
	ttl         time.Duration
	now         func() time.Time
}

// NewKeyRotator constructs a rotator. CurrentKey is required.
func NewKeyRotator(cfg KeyRotationConfig) (*KeyRotator, error) {
	if strings.TrimSpace(cfg.CurrentKey) == "" {
		return nil, fmt.Errorf("%w: %s current key required", ErrKeyRotationUnconfigured, cfg.CarrierName)
	}
	if cfg.TTL <= 0 {
		cfg.TTL = DefaultKeyRotationTTL
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	if cfg.PreviousSet.IsZero() && cfg.PreviousKey != "" {
		cfg.PreviousSet = cfg.Now()
	}
	return &KeyRotator{
		carrierName: cfg.CarrierName,
		currentKey:  cfg.CurrentKey,
		previousKey: cfg.PreviousKey,
		previousSet: cfg.PreviousSet,
		ttl:         cfg.TTL,
		now:         cfg.Now,
	}, nil
}

// VerifyResult describes which key matched.
type VerifyResult struct {
	Matched    bool
	MatchedKey string // "current" or "previous"
	KeyExpired bool
}

// Verify tries the current key first, then the previous key if
// within TTL. Returns typed errors for rejection/expiry.
func (kr *KeyRotator) Verify(verifyFn func(secret string) bool) (VerifyResult, error) {
	kr.mu.RLock()
	defer kr.mu.RUnlock()

	if kr.tryCurrentKey(verifyFn) {
		return VerifyResult{Matched: true, MatchedKey: "current"}, nil
	}
	return kr.tryPreviousKey(verifyFn)
}

func (kr *KeyRotator) tryCurrentKey(verifyFn func(string) bool) bool {
	return verifyFn(kr.currentKey)
}

func (kr *KeyRotator) tryPreviousKey(verifyFn func(string) bool) (VerifyResult, error) {
	if kr.previousKey == "" {
		return VerifyResult{}, ErrSignatureRejected
	}
	if kr.checkExpiry() {
		return VerifyResult{KeyExpired: true}, fmt.Errorf(
			"%w: carrier=%s ttl=%s", ErrPreviousKeyExpired, kr.carrierName, kr.ttl)
	}
	if verifyFn(kr.previousKey) {
		return VerifyResult{Matched: true, MatchedKey: "previous"}, nil
	}
	return VerifyResult{}, ErrSignatureRejected
}

func (kr *KeyRotator) checkExpiry() bool {
	if kr.previousSet.IsZero() {
		return true
	}
	return kr.now().Sub(kr.previousSet) > kr.ttl
}

// Rotate sets a new current key, demoting the old current to
// previous with a fresh TTL clock.
func (kr *KeyRotator) Rotate(newKey string) error {
	if strings.TrimSpace(newKey) == "" {
		return fmt.Errorf("%w: new key empty", ErrKeyRotationUnconfigured)
	}
	kr.mu.Lock()
	defer kr.mu.Unlock()
	kr.previousKey = kr.currentKey
	kr.previousSet = kr.now()
	kr.currentKey = newKey
	return nil
}

// KeyAge returns the age of the current key as a duration.
func (kr *KeyRotator) KeyAge() time.Duration {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	if kr.previousSet.IsZero() {
		return 0
	}
	return kr.now().Sub(kr.previousSet)
}

// PreviousKeyExpired returns true if the previous key TTL elapsed.
func (kr *KeyRotator) PreviousKeyExpired() bool {
	kr.mu.RLock()
	defer kr.mu.RUnlock()
	return kr.checkExpiry()
}

// CarrierName returns the carrier label.
func (kr *KeyRotator) CarrierName() string { return kr.carrierName }

// LoadKeyRotationTTL reads TTL from env, defaults to 48h.
func LoadKeyRotationTTL() time.Duration {
	if v := os.Getenv(EnvKeyRotationTTL); v != "" {
		if hours, err := strconv.Atoi(v); err == nil && hours > 0 {
			return time.Duration(hours) * time.Hour
		}
	}
	return DefaultKeyRotationTTL
}
