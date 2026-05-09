package social

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"
)

// testTikTokSecret is a deterministic 36-byte fixture used across
// the signing + webhook test suite. Above the 32-byte floor; safe
// to use across t.Parallel sub-tests.
const testTikTokSecret = "tiktok-shop-test-secret-bytes-fixture" // 37 bytes; gitleaks:allow

func TestComputeTikTokSignature_DeterministicCanonicalForm(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		path      string
		body      string
		timestamp int64
	}{
		{"empty body", "/api/products", "", 1700000000},
		{"json body", "/api/products", `{"hello":"world"}`, 1700000000},
		{"products list", "/api/products/search", "", 1700000123},
		{"long body", "/api/inventory/sync", strings.Repeat("a", 4096), 1700000999},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ComputeTikTokSignature(TikTokSignRequest{
				Secret:    []byte(testTikTokSecret),
				Timestamp: tc.timestamp,
				Path:      tc.path,
				Body:      []byte(tc.body),
			})
			if err != nil {
				t.Fatalf("ComputeTikTokSignature: %v", err)
			}
			want := referenceSignature(t, []byte(testTikTokSecret), tc.timestamp, tc.path, []byte(tc.body))
			if got != want {
				t.Fatalf("signature mismatch:\n got  %s\n want %s", got, want)
			}
		})
	}
}

// TestTikTokShopSigning_AgainstSpecVectors verifies our HMAC
// implementation against the canonical TikTok Shop spec vectors:
// the canonical form is "<timestamp>\n<path>\n<sha256-hex(body)>",
// and the signature is hex-HMAC-SHA256 of that string with the app
// client_secret as the key. Using a deterministic fixture so the
// test is independent of TikTok's live keys.
func TestTikTokShopSigning_AgainstSpecVectors(t *testing.T) {
	t.Parallel()

	const ts = int64(1700000111)
	const path = "/api/products/search"
	body := []byte(`{"page_size":20}`)

	bodyHash := sha256.Sum256(body)
	canonical := fmt.Sprintf("%d\n%s\n%s", ts, path, hex.EncodeToString(bodyHash[:]))
	mac := hmac.New(sha256.New, []byte(testTikTokSecret))
	_, _ = mac.Write([]byte(canonical))
	want := hex.EncodeToString(mac.Sum(nil))

	got, err := ComputeTikTokSignature(TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: ts,
		Path:      path,
		Body:      body,
	})
	if err != nil {
		t.Fatalf("ComputeTikTokSignature: %v", err)
	}
	if got != want {
		t.Fatalf("spec-vector signature mismatch:\n got  %s\n want %s", got, want)
	}
}

func TestComputeTikTokSignature_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	_, err := ComputeTikTokSignature(TikTokSignRequest{
		Secret:    []byte("too-short"),
		Timestamp: 1700000000,
		Path:      "/api/products",
	})
	if !errors.Is(err, ErrTikTokSecretTooShort) {
		t.Fatalf("err = %v, want ErrTikTokSecretTooShort", err)
	}
}

func TestVerifyTikTokSignature_ConstantTimeMatch(t *testing.T) {
	t.Parallel()

	const ts = int64(1700000222)
	body := []byte(`{"sku":"abc","delta":-1}`)
	want, err := ComputeTikTokSignature(TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: ts,
		Path:      "/api/inventory/sync",
		Body:      body,
	})
	if err != nil {
		t.Fatalf("ComputeTikTokSignature: %v", err)
	}
	if err := VerifyTikTokSignature(TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: ts,
		Path:      "/api/inventory/sync",
		Body:      body,
	}, want); err != nil {
		t.Fatalf("VerifyTikTokSignature: %v", err)
	}
}

func TestVerifyTikTokSignature_RejectsTamperedHex(t *testing.T) {
	t.Parallel()

	const ts = int64(1700000333)
	body := []byte(`{"sku":"abc"}`)
	good, err := ComputeTikTokSignature(TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: ts,
		Path:      "/api/products",
		Body:      body,
	})
	if err != nil {
		t.Fatalf("ComputeTikTokSignature: %v", err)
	}
	tampered := flipLastHexChar(good)
	err = VerifyTikTokSignature(TikTokSignRequest{
		Secret:    []byte(testTikTokSecret),
		Timestamp: ts,
		Path:      "/api/products",
		Body:      body,
	}, tampered)
	if !errors.Is(err, ErrTikTokSignatureMismatch) {
		t.Fatalf("err = %v, want ErrTikTokSignatureMismatch", err)
	}
}

func TestTikTokWebhookVerifier_ValidSignaturePasses(t *testing.T) {
	t.Parallel()

	v, err := NewTikTokWebhookVerifier(TikTokWebhookConfig{Secret: []byte(testTikTokSecret)})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	ts := now.Add(-time.Minute).Unix()
	body := []byte(`{"event":"order_placed","order_id":"abc-1"}`)
	header := v.SignWebhook(ts, body)
	if err := v.Verify(header, body); err != nil {
		t.Fatalf("Verify: %v", err)
	}
}

func TestTikTokWebhookVerifier_RejectsTamperedHeader(t *testing.T) {
	t.Parallel()

	v, err := NewTikTokWebhookVerifier(TikTokWebhookConfig{Secret: []byte(testTikTokSecret)})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	body := []byte(`{"event":"order_placed"}`)
	header := v.SignWebhook(now.Unix(), body)
	parts := strings.SplitN(header, "s=", 2)
	if len(parts) != 2 {
		t.Fatalf("unexpected header shape: %s", header)
	}
	tampered := parts[0] + "s=" + flipLastHexChar(parts[1])
	if err := v.Verify(tampered, body); !errors.Is(err, ErrTikTokSignatureMismatch) {
		t.Fatalf("err = %v, want ErrTikTokSignatureMismatch", err)
	}
}

func TestTikTokWebhookVerifier_RejectsExpired(t *testing.T) {
	t.Parallel()

	v, err := NewTikTokWebhookVerifier(TikTokWebhookConfig{Secret: []byte(testTikTokSecret), Tolerance: 30 * time.Second})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	now := time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC)
	v.now = func() time.Time { return now }
	expired := now.Add(-5 * time.Minute).Unix()
	body := []byte(`{"event":"order_placed"}`)
	header := v.SignWebhook(expired, body)
	if err := v.Verify(header, body); !errors.Is(err, ErrTikTokEventTooOld) {
		t.Fatalf("err = %v, want ErrTikTokEventTooOld", err)
	}
}

func TestTikTokWebhookVerifier_MalformedHeaders(t *testing.T) {
	t.Parallel()

	v, err := NewTikTokWebhookVerifier(TikTokWebhookConfig{Secret: []byte(testTikTokSecret)})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	cases := map[string]string{
		"empty":       "",
		"junk":        "not-a-header",
		"missing s":   "t=1700000000",
		"bad t":       "t=notanumber,s=deadbeef",
		"bad segment": "tonly,s=deadbeef",
	}
	for name, header := range cases {
		name, header := name, header
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			err := v.Verify(header, []byte("payload"))
			if err == nil {
				t.Fatalf("expected error for %q", header)
			}
		})
	}
}

func TestNewTikTokWebhookVerifier_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	if _, err := NewTikTokWebhookVerifier(TikTokWebhookConfig{Secret: []byte("too-short")}); !errors.Is(err, ErrTikTokSecretTooShort) {
		t.Fatalf("err = %v, want ErrTikTokSecretTooShort", err)
	}
}

func TestEnsureSecret_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrTikTokSecretTooShort},
		{"short", []byte("abc"), ErrTikTokSecretTooShort},
		{"min", []byte(strings.Repeat("a", 32)), nil},
		{"long", []byte(strings.Repeat("a", 64)), nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ensureSecret(tc.in)
			if tc.want == nil && err != nil {
				t.Fatalf("ensureSecret: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestErrSentinel_UnwrapsHelper(t *testing.T) {
	t.Parallel()
	wrapped := fmt.Errorf("wrapper: %w", ErrTikTokAuthFailed)
	if errSentinel(wrapped) != ErrTikTokAuthFailed {
		t.Fatalf("errSentinel did not unwrap %v", wrapped)
	}
}

// referenceSignature recomputes the canonical signature with the
// stdlib so a regression in ComputeTikTokSignature surfaces here.
func referenceSignature(t *testing.T, secret []byte, ts int64, path string, body []byte) string {
	t.Helper()
	bodyHash := sha256.Sum256(body)
	canonical := strconv.FormatInt(ts, 10) + "\n" + path + "\n" + hex.EncodeToString(bodyHash[:])
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(canonical))
	return hex.EncodeToString(mac.Sum(nil))
}

func flipLastHexChar(s string) string {
	if s == "" {
		return s
	}
	last := s[len(s)-1]
	switch last {
	case '0':
		last = '1'
	case 'f':
		last = 'e'
	default:
		last = '0'
	}
	return s[:len(s)-1] + string(last)
}
