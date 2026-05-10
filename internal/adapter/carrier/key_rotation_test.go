package carrier

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestKeyRotation_CurrentKeyValid(t *testing.T) {
	t.Parallel()
	kr, err := NewKeyRotator(KeyRotationConfig{
		CarrierName: "auspost",
		CurrentKey:  "current-secret",
		TTL:         48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	res, err := kr.Verify(func(secret string) bool { return secret == "current-secret" })
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Matched || res.MatchedKey != "current" {
		t.Fatalf("expected current match, got: %+v", res)
	}
}

func TestKeyRotation_PreviousKeyDuringRotation(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	kr, err := NewKeyRotator(KeyRotationConfig{
		CarrierName: "dhl",
		CurrentKey:  "new-secret",
		PreviousKey: "old-secret",
		PreviousSet: now,
		TTL:         48 * time.Hour,
		Now:         func() time.Time { return now.Add(1 * time.Hour) },
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	res, err := kr.Verify(func(secret string) bool { return secret == "old-secret" })
	if err != nil {
		t.Fatalf("Verify: %v", err)
	}
	if !res.Matched || res.MatchedKey != "previous" {
		t.Fatalf("expected previous match, got: %+v", res)
	}
}

func TestKeyRotation_ExpiredPreviousKeyRejected(t *testing.T) {
	t.Parallel()
	setTime := time.Date(2026, 5, 8, 0, 0, 0, 0, time.UTC)
	kr, err := NewKeyRotator(KeyRotationConfig{
		CarrierName: "auspost",
		CurrentKey:  "new-secret",
		PreviousKey: "old-secret",
		PreviousSet: setTime,
		TTL:         48 * time.Hour,
		Now:         func() time.Time { return setTime.Add(49 * time.Hour) },
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	_, err = kr.Verify(func(secret string) bool { return secret == "old-secret" })
	if !errors.Is(err, ErrPreviousKeyExpired) {
		t.Fatalf("expected ErrPreviousKeyExpired, got: %v", err)
	}
}

func TestKeyRotation_UnknownKeyRejected(t *testing.T) {
	t.Parallel()
	kr, err := NewKeyRotator(KeyRotationConfig{
		CarrierName: "dhl",
		CurrentKey:  "real-secret",
		TTL:         48 * time.Hour,
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	_, err = kr.Verify(func(secret string) bool { return secret == "unknown-secret" })
	if !errors.Is(err, ErrSignatureRejected) {
		t.Fatalf("expected ErrSignatureRejected, got: %v", err)
	}
}

func TestKeyRotation_RotateFlow(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	now := time.Date(2026, 5, 10, 0, 0, 0, 0, time.UTC)
	kr, err := NewKeyRotator(KeyRotationConfig{
		CarrierName: "auspost",
		CurrentKey:  "key-v1",
		TTL:         48 * time.Hour,
		Now:         func() time.Time { mu.Lock(); defer mu.Unlock(); return now },
	})
	if err != nil {
		t.Fatalf("NewKeyRotator: %v", err)
	}

	if err := kr.Rotate("key-v2"); err != nil {
		t.Fatalf("Rotate: %v", err)
	}

	res, err := kr.Verify(func(s string) bool { return s == "key-v2" })
	if err != nil || res.MatchedKey != "current" {
		t.Fatalf("key-v2 should match current: %+v %v", res, err)
	}
	res, err = kr.Verify(func(s string) bool { return s == "key-v1" })
	if err != nil || res.MatchedKey != "previous" {
		t.Fatalf("key-v1 should match previous: %+v %v", res, err)
	}

	mu.Lock()
	now = now.Add(49 * time.Hour)
	mu.Unlock()

	_, err = kr.Verify(func(s string) bool { return s == "key-v1" })
	if !errors.Is(err, ErrPreviousKeyExpired) {
		t.Fatalf("key-v1 should be expired, got: %v", err)
	}
}
