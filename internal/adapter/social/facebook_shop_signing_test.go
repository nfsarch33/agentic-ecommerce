package social

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"strings"
	"testing"
)

// testFacebookSecret is a deterministic 36-byte fixture used across
// the signing + webhook + appsecret_proof test suite. Above the
// 32-byte floor; safe to use across t.Parallel sub-tests.
const testFacebookSecret = "facebook-shop-test-secret-bytes-fix" // 35 bytes; gitleaks:allow

// TestComputeFacebookAppSecretProof_DeterministicHexSHA256 is the
// EC-4-2 RED acceptance test for the appsecret_proof primitive.
// The Graph API requires every authenticated call to carry
// `appsecret_proof = hex(HMAC-SHA256(access_token, app_secret))`.
func TestComputeFacebookAppSecretProof_DeterministicHexSHA256(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name        string
		accessToken string
	}{
		{"short token", "abc123"},
		{"page-style token", "EAAB12345-page-token-bytes-fixture-001"},
		{"long token", strings.Repeat("z", 256)},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ComputeFacebookAppSecretProof([]byte(testFacebookSecret), tc.accessToken)
			if err != nil {
				t.Fatalf("ComputeFacebookAppSecretProof: %v", err)
			}
			mac := hmac.New(sha256.New, []byte(testFacebookSecret))
			_, _ = mac.Write([]byte(tc.accessToken))
			want := hex.EncodeToString(mac.Sum(nil))
			if got != want {
				t.Fatalf("appsecret_proof mismatch:\n got  %s\n want %s", got, want)
			}
		})
	}
}

func TestComputeFacebookAppSecretProof_RejectsShortSecret(t *testing.T) {
	t.Parallel()
	_, err := ComputeFacebookAppSecretProof([]byte("too-short"), "any-token")
	if !errors.Is(err, ErrFacebookSecretTooShort) {
		t.Fatalf("err = %v, want ErrFacebookSecretTooShort", err)
	}
}

func TestComputeFacebookAppSecretProof_RejectsEmptyToken(t *testing.T) {
	t.Parallel()
	_, err := ComputeFacebookAppSecretProof([]byte(testFacebookSecret), "")
	if !errors.Is(err, ErrFacebookUnconfigured) {
		t.Fatalf("err = %v, want ErrFacebookUnconfigured", err)
	}
}

func TestVerifyFacebookWebhook_ValidSignaturePasses(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"object":"page","entry":[{"id":"123","time":1700000000}]}`)
	header, err := SignFacebookWebhook([]byte(testFacebookSecret), payload)
	if err != nil {
		t.Fatalf("SignFacebookWebhook: %v", err)
	}
	if !strings.HasPrefix(header, FacebookWebhookSignaturePrefix) {
		t.Fatalf("header missing prefix: %q", header)
	}
	if err := VerifyFacebookWebhook([]byte(testFacebookSecret), header, payload); err != nil {
		t.Fatalf("VerifyFacebookWebhook: %v", err)
	}
}

func TestVerifyFacebookWebhook_RejectsTamperedHex(t *testing.T) {
	t.Parallel()
	payload := []byte(`{"object":"page"}`)
	good, err := SignFacebookWebhook([]byte(testFacebookSecret), payload)
	if err != nil {
		t.Fatalf("SignFacebookWebhook: %v", err)
	}
	tampered := flipLastHexChar(good)
	err = VerifyFacebookWebhook([]byte(testFacebookSecret), tampered, payload)
	if !errors.Is(err, ErrFacebookSignatureMismatch) {
		t.Fatalf("err = %v, want ErrFacebookSignatureMismatch", err)
	}
}

func TestVerifyFacebookWebhook_RejectsMissingPrefix(t *testing.T) {
	t.Parallel()
	payload := []byte(`{}`)
	hexSig, err := ComputeFacebookWebhookSignature([]byte(testFacebookSecret), payload)
	if err != nil {
		t.Fatalf("ComputeFacebookWebhookSignature: %v", err)
	}
	err = VerifyFacebookWebhook([]byte(testFacebookSecret), hexSig, payload)
	if !errors.Is(err, ErrFacebookSignatureMismatch) {
		t.Fatalf("err = %v, want ErrFacebookSignatureMismatch", err)
	}
}

func TestVerifyFacebookWebhook_RejectsEmpty(t *testing.T) {
	t.Parallel()
	if err := VerifyFacebookWebhook([]byte(testFacebookSecret), "  ", []byte(`{}`)); !errors.Is(err, ErrFacebookSignatureMismatch) {
		t.Fatalf("err = %v, want ErrFacebookSignatureMismatch", err)
	}
}

func TestEnsureFacebookSecret_TableDriven(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		in   []byte
		want error
	}{
		{"empty", nil, ErrFacebookSecretTooShort},
		{"short", []byte("abc"), ErrFacebookSecretTooShort},
		{"min", []byte(strings.Repeat("a", 32)), nil},
		{"long", []byte(strings.Repeat("a", 64)), nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ensureFacebookSecret(tc.in)
			if tc.want == nil && err != nil {
				t.Fatalf("ensureFacebookSecret: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("err = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestErrFacebookUnwrap_Helper(t *testing.T) {
	t.Parallel()
	wrapped := errors.New("wrapper")
	if errFacebookUnwrap(wrapped) != nil {
		t.Fatalf("expected nil unwrap")
	}
}
