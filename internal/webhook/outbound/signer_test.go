package outbound

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"testing"
	"time"
)

func TestSignerAddsDeterministicHMACHeaders(t *testing.T) {
	t.Parallel()

	signer := NewSigner("whsec_test")
	body := []byte(`{"id":"evt-1","type":"product.created"}`)
	ts := time.Unix(1_779_000_000, 0).UTC()

	headers := signer.Sign("webhook-1", ts, body)

	wantMAC := hmac.New(sha256.New, []byte("whsec_test"))
	_, _ = wantMAC.Write([]byte("1779000000."))
	_, _ = wantMAC.Write(body)
	wantSignature := "sha256=" + hex.EncodeToString(wantMAC.Sum(nil))

	if got := headers.Get("X-EC-Webhook-ID"); got != "webhook-1" {
		t.Fatalf("webhook id header = %q, want webhook-1", got)
	}
	if got := headers.Get("X-EC-Webhook-Timestamp"); got != "1779000000" {
		t.Fatalf("timestamp header = %q, want 1779000000", got)
	}
	if got := headers.Get("X-EC-Webhook-Signature"); got != wantSignature {
		t.Fatalf("signature header = %q, want %q", got, wantSignature)
	}
}

func TestSignerCanonicalizesTimestampAndRawBodyBytes(t *testing.T) {
	t.Parallel()

	signer := NewSigner("whsec_test")
	ts := time.Date(2026, 5, 8, 3, 4, 5, 987654321, time.FixedZone("AEST", 10*60*60))

	headersA := signer.Sign("webhook-1", ts, []byte(`{"a":1,"b":2}`))
	headersB := signer.Sign("webhook-1", ts.UTC(), []byte(`{"b":2,"a":1}`))

	if got := headersA.Get("X-EC-Webhook-Timestamp"); got != "1778173445" {
		t.Fatalf("timestamp header = %q, want UTC unix seconds", got)
	}
	if headersA.Get("X-EC-Webhook-Signature") == headersB.Get("X-EC-Webhook-Signature") {
		t.Fatal("signatures matched for different raw JSON byte order")
	}
}

func TestSignerDoesNotExposeSecretInHeaderValues(t *testing.T) {
	t.Parallel()

	headers := NewSigner("super-secret-webhook-key").Sign("webhook-1", time.Unix(1, 0), []byte(`{}`))

	for key, values := range headers {
		for _, value := range values {
			if value == "super-secret-webhook-key" {
				t.Fatalf("%s exposed raw secret value", key)
			}
		}
	}
}
