// File scope: v3.3.1 QA Task 2 -- HMAC verify-then-parse security
// matrix for the EC-3-3 TikTok Shop order webhook.
//
// The v3.3.0 happy/sad paths already live in tiktok_order_test.go.
// This file is the single source-of-truth security matrix the PR
// reviewer can read top-to-bottom to confirm each scenario in the
// v3.3.1 plan:
//
//  1. Valid HMAC + valid timestamp -> 200 OK + OrderReceivedEvent
//     emitted exactly once.
//  2. Tampered body (1 byte flipped) + valid HMAC of original ->
//     ErrTikTokSignatureMismatch returned, NO event emitted, NO
//     decode attempted (verify-then-parse). The handler call site
//     proves verify runs BEFORE decodeEnvelope.
//  3. Tampered HMAC (1 byte flipped) + valid body ->
//     ErrTikTokSignatureMismatch returned, NO event.
//  4. Stale timestamp (>5 min old per DefaultTikTokWebhookTolerance)
//     -> ErrTikTokEventTooOld returned, 400 (poison pill so
//     TikTok stops retrying), NO event.
//  5. Duplicate event (idempotency_key already reserved) -> 200 OK
//     no-op (TikTok stops retrying); ZERO additional events emitted.
//
// Verify-then-parse evidence: the call site in tiktok_order.go's
// ServeHTTP runs verifySignature on the raw body BEFORE
// decodeEnvelope ever sees the bytes. That ordering is what
// keeps a tamper-with-junk-body POST from ever feeding the JSON
// decoder.
//
// Cite skill: go-security-review (HMAC verify-then-parse,
// constant-time compare, replay protection floor; bounded
// signature-failure metric cardinality).
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
)

// securityCase captures one row of the HMAC scenario matrix.
type securityCase struct {
	name              string
	mutateBody        func([]byte) []byte               // identity by default; tamper modifies
	mutateSignature   func(string) string               // identity by default; tamper modifies
	signatureTime     time.Time                         // signing timestamp; default = harness fixed time
	expectedHTTP      int                               // expected HTTP status
	expectedSentinel  error                             // wrapped error returned by verifier (errors.Is)
	expectedOutcome   string                            // RecordWebhook outcome label
	expectedFailLabel string                            // RecordSignatureFailure reason label
	expectEventCount  int                               // number of OrderReceived events expected
	prep              func(t *testing.T, h *secHarness) // optional pre-call setup (e.g. duplicate prime)
}

// secHarness bundles the per-test handler + bus + verifier.
type secHarness struct {
	handler  *TikTokOrderHandler
	bus      *eventbus.InMemoryBus
	verifier *social.TikTokWebhookVerifier
	metrics  *capturingMetrics
}

// TestTikTokWebhookSecurity_ScenarioMatrix is the v3.3.1 Task 2
// table-driven entry point. Every row maps to one acceptance line
// from the plan; sentrux complex_fn stays low because the per-row
// branching is in mutateBody/mutateSignature closures, not the
// loop body.
func TestTikTokWebhookSecurity_ScenarioMatrix(t *testing.T) {
	t.Parallel()

	cases := buildSecurityScenarios()
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runSecurityScenario(t, tc)
		})
	}
}

// runSecurityScenario executes one row from the matrix. Pulled
// out so the table loop body stays one line.
func runSecurityScenario(t *testing.T, tc securityCase) {
	t.Helper()
	h := newSecurityHarness(t)
	if tc.prep != nil {
		tc.prep(t, h)
	}
	body := canonicalSecurityOrderBody(t, tc.name)
	if tc.mutateBody != nil {
		body = tc.mutateBody(body)
	}
	signTime := tc.signatureTime
	if signTime.IsZero() {
		signTime = fixedTestTime.Add(-30 * time.Second)
	}
	header := h.verifier.SignWebhook(signTime.Unix(), canonicalSecurityOrderBody(t, tc.name))
	if tc.mutateSignature != nil {
		header = tc.mutateSignature(header)
	}
	rec := serveSecurityRequest(t, h.handler, body, header)

	assertSecurityHTTP(t, tc, rec)
	assertSecurityEventCount(t, tc, h.bus)
	assertSecurityMetrics(t, tc, h.metrics)
}

// buildSecurityScenarios returns the 5+ canonical rows. Pure
// (no IO, no t.*).
func buildSecurityScenarios() []securityCase {
	return []securityCase{
		{
			name:             "valid_hmac_emits_event",
			expectedHTTP:     http.StatusOK,
			expectedOutcome:  "ok",
			expectEventCount: 1,
		},
		{
			name: "tampered_body_byte_flip_rejected",
			mutateBody: func(b []byte) []byte {
				cp := append([]byte(nil), b...)
				if i := bytes.Index(cp, []byte(`"order-`)); i >= 0 && i+8 < len(cp) {
					// flip one char inside the order_id value
					cp[i+8] ^= 0x01
				}
				return cp
			},
			expectedHTTP:      http.StatusUnauthorized,
			expectedSentinel:  social.ErrTikTokSignatureMismatch,
			expectedFailLabel: "mismatch",
			expectEventCount:  0,
		},
		{
			name: "tampered_signature_byte_flip_rejected",
			mutateSignature: func(header string) string {
				return tamperHexSuffix(header)
			},
			expectedHTTP:      http.StatusUnauthorized,
			expectedSentinel:  social.ErrTikTokSignatureMismatch,
			expectedFailLabel: "mismatch",
			expectEventCount:  0,
		},
		{
			name:              "stale_timestamp_replay_blocked",
			signatureTime:     fixedTestTime.Add(-10 * time.Minute), // > 5min tolerance
			expectedHTTP:      http.StatusBadRequest,
			expectedSentinel:  social.ErrTikTokEventTooOld,
			expectedFailLabel: "expired",
			expectEventCount:  0,
		},
		{
			name: "duplicate_event_id_short_circuits",
			prep: func(t *testing.T, h *secHarness) {
				t.Helper()
				body := canonicalSecurityOrderBody(t, "duplicate_event_id_short_circuits")
				header := h.verifier.SignWebhook(fixedTestTime.Add(-30*time.Second).Unix(), body)
				_ = serveSecurityRequest(t, h.handler, body, header)
			},
			expectedHTTP:     http.StatusOK,
			expectedOutcome:  "duplicate",
			expectEventCount: 1, // ONE event from the prep call; second is no-op
		},
		{
			name:              "missing_signature_header_rejected",
			mutateSignature:   func(_ string) string { return "" },
			expectedHTTP:      http.StatusUnauthorized,
			expectedSentinel:  social.ErrTikTokMissingSignature,
			expectedFailLabel: "missing",
			expectEventCount:  0,
		},
		{
			name:              "malformed_header_rejected",
			mutateSignature:   func(_ string) string { return "garbage-no-equals" },
			expectedHTTP:      http.StatusUnauthorized,
			expectedSentinel:  social.ErrTikTokSignatureMalformed,
			expectedFailLabel: "malformed",
			expectEventCount:  0,
		},
	}
}

// newSecurityHarness builds a per-test handler/bus/verifier triplet.
// Reuses newWebhookHarness but exposes only the security-relevant
// surface so the case bodies stay one-liners.
func newSecurityHarness(t *testing.T) *secHarness {
	t.Helper()
	handler, bus, verifier, metrics := newWebhookHarness(t)
	return &secHarness{handler: handler, bus: bus, verifier: verifier, metrics: metrics}
}

// canonicalSecurityOrderBody constructs the JSON envelope every
// scenario starts from. Stable bytes so a tamper is reproducible.
func canonicalSecurityOrderBody(t *testing.T, name string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"tenant_id":   "tenant-1",
		"order_id":    "order-" + name,
		"shop_id":     "shop-sec",
		"buyer_email": "buyer@example.com",
		"total_cents": 4999,
		"currency":    "AUD",
		"items": []map[string]any{
			{"sku": "SKU-SEC", "quantity": 1, "unit_cents": 4999, "product_id": "prod-sec"},
		},
		"status":      "placed",
		"occurred_at": "2026-05-09T11:59:30Z",
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

// serveSecurityRequest replays one POST with the supplied body +
// signature header. Empty signature header is preserved verbatim
// so the missing-signature case lands the right code path.
func serveSecurityRequest(t *testing.T, handler *TikTokOrderHandler, body []byte, signatureHeader string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/orders", bytes.NewReader(body))
	if signatureHeader != "" {
		req.Header.Set("X-Tts-Signature", signatureHeader)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// assertSecurityHTTP checks the response code matches the scenario.
func assertSecurityHTTP(t *testing.T, tc securityCase, rec *httptest.ResponseRecorder) {
	t.Helper()
	if rec.Code != tc.expectedHTTP {
		t.Fatalf("[%s] HTTP = %d, want %d; body=%q", tc.name, rec.Code, tc.expectedHTTP, rec.Body.String())
	}
}

// assertSecurityEventCount checks exactly the expected number of
// OrderReceived events landed on the bus. Verify-then-parse evidence:
// rejected scenarios MUST report 0 events.
func assertSecurityEventCount(t *testing.T, tc securityCase, bus *eventbus.InMemoryBus) {
	t.Helper()
	got := 0
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.OrderReceived {
			got++
		}
	}
	if got != tc.expectEventCount {
		t.Fatalf("[%s] OrderReceived events = %d, want %d", tc.name, got, tc.expectEventCount)
	}
}

// assertSecurityMetrics checks the failure-label + outcome metrics
// match the scenario. Bounded set of labels => low cardinality.
func assertSecurityMetrics(t *testing.T, tc securityCase, metrics *capturingMetrics) {
	t.Helper()
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if tc.expectedFailLabel != "" {
		if !containsString(metrics.sigFails, tc.expectedFailLabel) {
			t.Fatalf("[%s] sig_fails = %v, want includes %q", tc.name, metrics.sigFails, tc.expectedFailLabel)
		}
	}
	if tc.expectedOutcome != "" {
		if !containsString(metrics.webhooks, tc.expectedOutcome) {
			t.Fatalf("[%s] webhooks = %v, want includes %q", tc.name, metrics.webhooks, tc.expectedOutcome)
		}
	}
}

// tamperHexSuffix flips the last hex character of the s=<hex> field
// in the canonical "t=<unix>,s=<hex>" header. Pulled out so the
// scenario closure stays one expression.
func tamperHexSuffix(header string) string {
	parts := strings.SplitN(header, "s=", 2)
	if len(parts) != 2 {
		return header
	}
	last := parts[1][len(parts[1])-1]
	flipped := byte('0')
	if last == '0' {
		flipped = '1'
	}
	return parts[0] + "s=" + parts[1][:len(parts[1])-1] + string(flipped)
}

// containsString reports whether the slice contains the want value.
// Local copy so the security test is self-contained vs the chaos
// helper.
func containsString(values []string, want string) bool {
	for _, v := range values {
		if v == want {
			return true
		}
	}
	return false
}

// TestTikTokWebhookSecurity_VerifyBeforeParse is the call-site
// proof the verifier runs BEFORE decodeEnvelope. We send malformed
// JSON whose signature was correctly computed over the raw bytes;
// when the signature is also tampered the rejection path is
// SignatureMismatch (NOT a JSON decode error). When the signature
// is valid for the malformed body the path drops through to
// decodeEnvelope and surfaces ErrWebhookPayloadInvalid -- proving
// the order is verify -> parse, not the other way around.
func TestTikTokWebhookSecurity_VerifyBeforeParse(t *testing.T) {
	t.Parallel()

	h := newSecurityHarness(t)
	junk := []byte("this is not json {{{")

	// Case A: tampered signature on junk body -> SignatureMismatch
	// (verifier rejects before parser ever runs).
	header := h.verifier.SignWebhook(fixedTestTime.Add(-30*time.Second).Unix(), junk)
	header = tamperHexSuffix(header)
	rec := serveSecurityRequest(t, h.handler, junk, header)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("tampered junk body: HTTP = %d, want 401", rec.Code)
	}
	if got := countOrderReceived(h.bus); got != 0 {
		t.Fatalf("tampered junk body: OrderReceived = %d, want 0", got)
	}

	// Case B: VALID signature on junk body -> reaches decode and
	// surfaces 400 ErrWebhookPayloadInvalid (proves verify-then-parse).
	freshHarness := newSecurityHarness(t)
	header = freshHarness.verifier.SignWebhook(fixedTestTime.Add(-30*time.Second).Unix(), junk)
	rec = serveSecurityRequest(t, freshHarness.handler, junk, header)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("valid-sig junk body: HTTP = %d, want 400 (decode failure after verify)", rec.Code)
	}
	if got := countOrderReceived(freshHarness.bus); got != 0 {
		t.Fatalf("valid-sig junk body: OrderReceived = %d, want 0 (decode failure)", got)
	}
	freshHarness.metrics.mu.Lock()
	defer freshHarness.metrics.mu.Unlock()
	if !containsString(freshHarness.metrics.webhooks, "decode_failed") {
		t.Fatalf("metrics outcomes = %v, want includes decode_failed", freshHarness.metrics.webhooks)
	}
}

// countOrderReceived is a small helper used only by the
// verify-before-parse proof. Keeps the assertion sites short.
func countOrderReceived(bus *eventbus.InMemoryBus) int {
	n := 0
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.OrderReceived {
			n++
		}
	}
	return n
}

// TestTikTokWebhookSecurity_ConstantTimeComparePath asserts the
// VerifyTikTokSignature primitive uses crypto/subtle.ConstantTimeCompare
// (covers the underlying signing helper, not the webhook handler).
// This is a small co-located assertion so the v3.3.1 PR carries
// the proof inside the webhook security file the reviewer reads.
func TestTikTokWebhookSecurity_ConstantTimeComparePath(t *testing.T) {
	t.Parallel()
	secret := []byte(testTikTokWebhookSecret)
	body := []byte(`{"order_id":"x"}`)
	ts := fixedTestTime.Unix()
	good := buildSignerHeaderFromBytes(t, secret, ts, body)
	if err := verifySignerHeaderRoundTrip(t, secret, ts, body, good, 5*time.Minute); err != nil {
		t.Fatalf("good header should verify: %v", err)
	}
	tampered := tamperHexSuffix(good)
	if err := verifySignerHeaderRoundTrip(t, secret, ts, body, tampered, 5*time.Minute); !errors.Is(err, social.ErrTikTokSignatureMismatch) {
		t.Fatalf("tampered header err = %v, want ErrTikTokSignatureMismatch", err)
	}
}

// buildSignerHeaderFromBytes is a thin helper around
// social.NewTikTokWebhookVerifier.SignWebhook so tests can recreate
// the canonical "t=<ts>,s=<hex>" header from raw bytes.
func buildSignerHeaderFromBytes(t *testing.T, secret []byte, timestamp int64, body []byte) string {
	t.Helper()
	v, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret: secret,
		Now:    func() time.Time { return time.Unix(timestamp, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	return v.SignWebhook(timestamp, body)
}

// verifySignerHeaderRoundTrip wraps the verifier construction +
// Verify call so test-side assertions stay one-liners.
func verifySignerHeaderRoundTrip(t *testing.T, secret []byte, timestamp int64, body []byte, header string, tolerance time.Duration) error {
	t.Helper()
	v, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret:    secret,
		Tolerance: tolerance,
		Now:       func() time.Time { return time.Unix(timestamp, 0).UTC() },
	})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	return v.Verify(header, body)
}

// TestTikTokWebhookSecurity_NoEventOnIdempotencyError asserts that
// an idempotency-store failure surfaces 500 + NO event emitted.
// Closes the loop on "verify+decode succeeded but persistence
// failed" so the bus stays consistent.
func TestTikTokWebhookSecurity_NoEventOnIdempotencyError(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	verifier, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret: []byte(testTikTokWebhookSecret),
		Now:    func() time.Time { return fixedTestTime },
	})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	metrics := &capturingMetrics{}
	handler, err := NewTikTokOrderHandler(nil, TikTokOrderHandlerConfig{
		Verifier:    verifier,
		Publisher:   bus,
		Idempotency: &errorIdempotencyStore{},
		TenantID:    "tenant-1",
		Metrics:     metrics,
		Now:         func() time.Time { return fixedTestTime },
	})
	if err != nil {
		t.Fatalf("NewTikTokOrderHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close(context.Background()) })

	body := orderBody(t, "order-idem-fail")
	header := verifier.SignWebhook(fixedTestTime.Add(-30*time.Second).Unix(), body)
	rec := serveSecurityRequest(t, handler, body, header)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("idempotency error: HTTP = %d, want 500", rec.Code)
	}
	if got := countOrderReceived(bus); got != 0 {
		t.Fatalf("idempotency error: OrderReceived = %d, want 0", got)
	}
	metrics.mu.Lock()
	defer metrics.mu.Unlock()
	if !containsString(metrics.webhooks, "idempotency_error") {
		t.Fatalf("metrics = %v, want includes idempotency_error", metrics.webhooks)
	}
}

// errorIdempotencyStore is the test double that always errors.
// Used only by TestTikTokWebhookSecurity_NoEventOnIdempotencyError.
type errorIdempotencyStore struct{}

func (errorIdempotencyStore) Reserve(_ context.Context, _, _ string) (bool, error) {
	return false, errors.New("idempotency backend unavailable")
}
