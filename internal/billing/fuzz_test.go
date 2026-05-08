// Fuzz harness for the Stripe webhook signature verifier. The contract
// is "must NEVER panic on attacker-controlled signature header bytes
// or payload bytes; must always return an error rather than crash".
// Seeds include a structurally valid header, missing-key segments,
// oversized values, and embedded control bytes.

package billing

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strconv"
	"strings"
	"testing"
	"time"
)

const fuzzVerifierSecret = "fuzz-secret-at-least-32-bytes-long-yes"

func newFuzzVerifier() *WebhookVerifier {
	v, err := NewWebhookVerifier(WebhookConfig{
		Secret:    []byte(fuzzVerifierSecret),
		Tolerance: 5 * time.Minute,
		Now:       func() time.Time { return time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		panic(err)
	}
	return v
}

func FuzzWebhookVerify(f *testing.F) {
	verifier := newFuzzVerifier()

	now := time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC).Unix()
	payload := []byte(`{"id":"evt_1","type":"customer.subscription.created"}`)
	mac := hmac.New(sha256.New, []byte(fuzzVerifierSecret))
	_, _ = mac.Write([]byte(strconv.FormatInt(now, 10)))
	_, _ = mac.Write([]byte("."))
	_, _ = mac.Write(payload)
	good := "t=" + strconv.FormatInt(now, 10) + ",v1=" + hex.EncodeToString(mac.Sum(nil))

	f.Add(good, payload)
	f.Add("", []byte(""))
	f.Add("v1=abcd", []byte("payload"))
	f.Add("t=notanumber,v1=abc", []byte("payload"))
	f.Add("t=1700000000,v1=", []byte(""))
	f.Add("t=1700000000,v1=zz", []byte("payload"))
	f.Add(strings.Repeat("v1=a,", 100)+"t=1700000000", []byte("x"))
	f.Add("t=1700000000,v1=abc,v1=def,v1=ghi", []byte{})
	f.Add("t=-1,v1=abc", []byte("a"))
	f.Add("\x00\x01\x02", []byte("\x00\x01\x02"))

	f.Fuzz(func(t *testing.T, header string, payload []byte) {
		_ = verifier.Verify(header, payload)
	})
}
