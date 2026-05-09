package session

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestResolveMasterKey_RejectsEmpty(t *testing.T) {
	t.Parallel()
	_, err := resolveMasterKey("")
	if !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("want ErrInvalidMasterKey, got %v", err)
	}
}

func TestResolveMasterKey_RejectsBadHex(t *testing.T) {
	t.Parallel()
	_, err := resolveMasterKey("not-hex")
	if !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("want ErrInvalidMasterKey, got %v", err)
	}
}

func TestResolveMasterKey_RejectsShort(t *testing.T) {
	t.Parallel()
	_, err := resolveMasterKey("0011")
	if !errors.Is(err, ErrInvalidMasterKey) {
		t.Fatalf("want ErrInvalidMasterKey, got %v", err)
	}
}

func TestResolveMasterKey_AcceptsCanonical(t *testing.T) {
	t.Parallel()
	hex32 := strings.Repeat("ab", 32)
	raw, err := resolveMasterKey(hex32)
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if len(raw) != MasterKeyBytes {
		t.Fatalf("want %d bytes, got %d", MasterKeyBytes, len(raw))
	}
}

func TestValidateKey_AcceptsClean(t *testing.T) {
	t.Parallel()
	if err := validateKey("tenant-a", "rednote"); err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
}

func TestValidateKey_RejectsEmptyTenant(t *testing.T) {
	t.Parallel()
	if err := validateKey("", "rednote"); !errors.Is(err, ErrInvalidTenantID) {
		t.Fatalf("want ErrInvalidTenantID, got %v", err)
	}
}

func TestValidateKey_RejectsSeparator(t *testing.T) {
	t.Parallel()
	if err := validateKey("tenant:a", "rednote"); !errors.Is(err, ErrInvalidTenantID) {
		t.Fatalf("want ErrInvalidTenantID, got %v", err)
	}
	if err := validateKey("tenant", "rednote:bad"); !errors.Is(err, ErrInvalidChannel) {
		t.Fatalf("want ErrInvalidChannel, got %v", err)
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, 0xab)
	plaintext := []byte("hello world")
	ct, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	got, err := decrypt(key, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if string(got) != string(plaintext) {
		t.Fatalf("roundtrip mismatch: %q vs %q", got, plaintext)
	}
}

func TestDecrypt_TamperDetected(t *testing.T) {
	t.Parallel()
	key := newTestKey(t, 0xcd)
	plaintext := []byte("secret-cookie-jar")
	ct, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	ct[len(ct)-1] ^= 0x01
	if _, err := decrypt(key, ct); !errors.Is(err, ErrSessionTampered) {
		t.Fatalf("want ErrSessionTampered, got %v", err)
	}
}

func TestCheckExpiry_Recent(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	recorded := now.Add(-1 * time.Hour)
	if err := checkExpiry(recorded, now, MaxSessionAge); err != nil {
		t.Fatalf("unexpected expiry: %v", err)
	}
}

func TestCheckExpiry_Old(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	recorded := now.Add(-31 * 24 * time.Hour)
	if err := checkExpiry(recorded, now, MaxSessionAge); !errors.Is(err, ErrSessionExpired) {
		t.Fatalf("want ErrSessionExpired, got %v", err)
	}
}

func TestComposeDecomposeAccount_RoundTrip(t *testing.T) {
	t.Parallel()
	want := "tenant-foo:rednote"
	got := composeAccount("tenant-foo", "rednote")
	if got != want {
		t.Fatalf("composeAccount: want %q got %q", want, got)
	}
	tenant, channel, ok := decomposeAccount(got)
	if !ok || tenant != "tenant-foo" || channel != "rednote" {
		t.Fatalf("decomposeAccount: tenant=%q channel=%q ok=%v", tenant, channel, ok)
	}
	if _, _, ok := decomposeAccount("no-separator"); ok {
		t.Fatalf("decomposeAccount should reject string without separator")
	}
}

func TestSerializeDeserialize_RoundTrip(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	in := SessionBlob{
		Cookies: []map[string]any{{
			"name":   "session_id",
			"value":  "abc123",
			"domain": ".rednote.com",
		}},
		LocalStorage: map[string]string{"theme": "dark"},
		UserAgent:    "test/1.0",
		RecordedAt:   now,
	}
	raw, err := serialize(in, now)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := deserialize(raw)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if out.UserAgent != in.UserAgent {
		t.Fatalf("ua mismatch")
	}
	if !out.RecordedAt.Equal(in.RecordedAt) {
		t.Fatalf("recorded_at mismatch: %v vs %v", out.RecordedAt, in.RecordedAt)
	}
}

func TestSerialize_ZeroRecordedAtFilledFromNow(t *testing.T) {
	t.Parallel()
	now := time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)
	raw, err := serialize(SessionBlob{}, now)
	if err != nil {
		t.Fatalf("serialize: %v", err)
	}
	out, err := deserialize(raw)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if !out.RecordedAt.Equal(now) {
		t.Fatalf("zero recorded_at should default to now: got %v", out.RecordedAt)
	}
}

// newTestKey returns a deterministic 32-byte AES key for tests.
func newTestKey(t *testing.T, seed byte) []byte {
	t.Helper()
	out := make([]byte, MasterKeyBytes)
	for i := range out {
		out[i] = seed + byte(i)
	}
	return out
}

// helper used by file_store_test.go + keychain_macos_test.go.
func newTestContext(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	return context.WithTimeout(context.Background(), 10*time.Second)
}
