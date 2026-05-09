// File scope: v3.6.0 EC-8-3 messaging webhook RED tests.
//
// Cite plan EC-8-3 acceptance:
//   - TikTok HMAC verify-then-parse (mirror v3.3.0)
//   - Facebook X-Hub-Signature-256 verify (mirror v3.4.0)
//   - Idempotency on message_id + channel
//   - Pipeline: webhook -> idempotency -> classifier -> responder
//     -> reply via outbound channel client -> audit log
//   - Typed events CustomerMessage{Received,Replied,Escalated}
//   - Typed errors ErrInvalidMessageHMAC, ErrMessageDuplicate,
//     ErrChannelSendFailed
package webhook

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/adapter/social"
	"github.com/nfsarch33/agentic-ecommerce/internal/agent/customerservice"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/port"
)

const testMessageWebhookSecret = "tiktok-message-webhook-test-secret-bytes-fixture" // gitleaks:allow
const testFacebookAppSecret = "facebook-app-secret-test-fixture-bytes-32"           // gitleaks:allow

// stubLLM implements port.AITextGenerator with a configured
// response/error pair.
type stubLLM struct {
	response port.AICompletionResponse
	err      error
}

func (s *stubLLM) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return s.response, s.err
}

// recordingSender is the in-test port.OutboundMessageSender; it
// records every Send call so the audit log is verifiable.
type recordingSender struct {
	calls atomic.Int32
	last  port.OutboundMessageRequest
	err   error
}

func (s *recordingSender) SendMessage(_ context.Context, req port.OutboundMessageRequest) (port.OutboundMessageResponse, error) {
	s.calls.Add(1)
	s.last = req
	if s.err != nil {
		return port.OutboundMessageResponse{}, s.err
	}
	return port.OutboundMessageResponse{ProviderMessageID: "provider-" + req.ThreadID}, nil
}

// inMemoryFAQStore drops in for FAQResponder.Store in test wiring.
type inMemoryFAQStore struct {
	entries []customerservice.FAQEntry
}

func (s *inMemoryFAQStore) Search(_ context.Context, query customerservice.FAQSearchQuery) ([]customerservice.FAQEntry, error) {
	out := make([]customerservice.FAQEntry, 0, len(s.entries))
	for _, e := range s.entries {
		if e.TenantID != query.TenantID {
			continue
		}
		if query.Language != "" && e.Language != query.Language {
			continue
		}
		if query.Intent != "" && e.IntentCategory != query.Intent {
			continue
		}
		out = append(out, e)
	}
	return out, nil
}

// recordingMessagingMetrics records every emitted webhook outcome.
type recordingMessagingMetrics struct {
	outcomes []string
}

func (m *recordingMessagingMetrics) RecordMessageWebhook(_, _, status string) {
	m.outcomes = append(m.outcomes, status)
}

var fixedMessagingTime = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

// faqFixtures returns one shipping_query FAQ so the auto-reply
// path is exercisable end-to-end.
func faqFixtures() []customerservice.FAQEntry {
	return []customerservice.FAQEntry{
		{
			TenantID: "tenant-cs", EntryID: "faq-shipping-sydney",
			Language: customerservice.LanguageEN, IntentCategory: customerservice.IntentShippingQuery,
			Question: "How long does shipping to Sydney take?",
			Answer:   "Sydney metro deliveries arrive in 3-5 business days for standard shipping.",
			Keywords: []string{"shipping", "sydney", "delivery"},
		},
	}
}

// classifierLLMResponse builds the LLM-side classifier JSON.
func classifierLLMResponse(intent customerservice.Intent, sentiment customerservice.Sentiment, lang customerservice.Language, conf float64) string {
	body, _ := json.Marshal(map[string]any{
		"intent":     string(intent),
		"sentiment":  string(sentiment),
		"language":   string(lang),
		"confidence": conf,
	})
	return string(body)
}

// buildPipeline wires a shared messaging pipeline with the supplied
// LLM behaviour + sender + bus.
func buildPipeline(t *testing.T, llmContent string, llmErr error, sender *recordingSender, channel string, bus *eventbus.InMemoryBus) (*MessagingPipeline, *customerservice.EnquiryClassifier, *customerservice.FAQResponder) {
	t.Helper()
	llm := &stubLLM{response: port.AICompletionResponse{Content: llmContent}, err: llmErr}
	classifier, err := customerservice.NewEnquiryClassifier(nil, customerservice.EnquiryClassifierConfig{
		Generator: llm,
		TenantID:  "tenant-cs",
		Now:       func() time.Time { return fixedMessagingTime },
	})
	if err != nil {
		t.Fatalf("NewEnquiryClassifier: %v", err)
	}
	t.Cleanup(func() { _ = classifier.Close(context.Background()) })

	responder, err := customerservice.NewFAQResponder(nil, customerservice.FAQResponderConfig{
		Generator: llm,
		Store:     &inMemoryFAQStore{entries: faqFixtures()},
		TenantID:  "tenant-cs",
		Now:       func() time.Time { return fixedMessagingTime },
	})
	if err != nil {
		t.Fatalf("NewFAQResponder: %v", err)
	}
	t.Cleanup(func() { _ = responder.Close(context.Background()) })

	pipeline, err := NewMessagingPipeline(nil, MessagingPipelineConfig{
		Classifier: classifier,
		Responder:  responder,
		Senders:    map[string]port.OutboundMessageSender{channel: sender},
		Publisher:  bus,
		Now:        func() time.Time { return fixedMessagingTime },
	})
	if err != nil {
		t.Fatalf("NewMessagingPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	return pipeline, classifier, responder
}

// buildTikTokMessageHandler wires the channel-specific TikTok handler.
func buildTikTokMessageHandler(t *testing.T, pipeline *MessagingPipeline, metrics *recordingMessagingMetrics) (*TikTokMessageHandler, *social.TikTokWebhookVerifier) {
	t.Helper()
	verifier, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret: []byte(testMessageWebhookSecret),
		Now:    func() time.Time { return fixedMessagingTime },
	})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	handler, err := NewTikTokMessageHandler(nil, TikTokMessageHandlerConfig{
		Verifier:    verifier,
		Pipeline:    pipeline,
		Idempotency: NewMemoryIdempotencyStore(),
		TenantID:    "tenant-cs",
		Channel:     "tiktok",
		Metrics:     metrics,
		Now:         func() time.Time { return fixedMessagingTime },
	})
	if err != nil {
		t.Fatalf("NewTikTokMessageHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close(context.Background()) })
	return handler, verifier
}

func tikTokMessageBody(t *testing.T, messageID string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"tenant_id":   "tenant-cs",
		"message_id":  messageID,
		"thread_id":   "thread-123",
		"buyer_id":    "buyer-456",
		"text":        "How long does shipping to Sydney take?",
		"occurred_at": fixedMessagingTime.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

func signedTikTokMessageRequest(t *testing.T, body []byte, verifier *social.TikTokWebhookVerifier) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/messages", bytes.NewReader(body))
	req.Header.Set("X-Tts-Signature", verifier.SignWebhook(fixedMessagingTime.Add(-30*time.Second).Unix(), body))
	return req
}

// TestTikTokMessageWebhook_VerifiesHMACBeforeParse covers the
// verify-then-parse pattern: a tampered body returns 401 and no
// CustomerMessageReceived event is emitted.
func TestTikTokMessageWebhook_VerifiesHMACBeforeParse(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	pipeline, _, _ := buildPipeline(t, classifierLLMResponse(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.92), nil, sender, "tiktok", bus)
	handler, verifier := buildTikTokMessageHandler(t, pipeline, &recordingMessagingMetrics{})

	body := tikTokMessageBody(t, "m1")
	req := signedTikTokMessageRequest(t, body, verifier)
	// Tamper the body AFTER signing.
	tamperedBody := append([]byte{}, body...)
	tamperedBody[len(tamperedBody)-2] = 'X'
	req2 := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/messages", bytes.NewReader(tamperedBody))
	req2.Header.Set("X-Tts-Signature", req.Header.Get("X-Tts-Signature"))

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req2)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (tampered HMAC)", rec.Code)
	}
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.CustomerMessageReceived {
			t.Fatalf("unexpected CustomerMessageReceived on tampered body")
		}
	}
}

func TestTikTokMessageWebhook_IdempotentRetryReturnsCached(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	metrics := &recordingMessagingMetrics{}
	pipeline, _, _ := buildPipeline(t, "Sydney 3-5 days", nil, sender, "tiktok", bus)
	handler, verifier := buildTikTokMessageHandler(t, pipeline, metrics)

	body := tikTokMessageBody(t, "m-dup")
	for i := 0; i < 3; i++ {
		req := signedTikTokMessageRequest(t, body, verifier)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d", i, rec.Code)
		}
	}
	receivedCount := 0
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.CustomerMessageReceived {
			receivedCount++
		}
	}
	if receivedCount != 1 {
		t.Fatalf("CustomerMessageReceived count = %d, want 1 (idempotent)", receivedCount)
	}
	dups := 0
	for _, o := range metrics.outcomes {
		if o == "duplicate" {
			dups++
		}
	}
	if dups != 2 {
		t.Fatalf("duplicate outcomes = %d, want 2", dups)
	}
}

func TestMessageWebhook_TriggersEndToEndPipeline(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	pipeline, _, _ := buildPipeline(t, classifierLLMResponse(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.92), nil, sender, "tiktok", bus)
	handler, verifier := buildTikTokMessageHandler(t, pipeline, &recordingMessagingMetrics{})

	body := tikTokMessageBody(t, "m-pipeline")
	req := signedTikTokMessageRequest(t, body, verifier)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d body=%s", rec.Code, rec.Body.String())
	}

	if sender.calls.Load() != 1 {
		t.Fatalf("sender calls = %d, want 1", sender.calls.Load())
	}
	if sender.last.ThreadID != "thread-123" {
		t.Fatalf("ThreadID = %q, want thread-123", sender.last.ThreadID)
	}

	gotReceived, gotReplied := false, false
	for _, e := range bus.Delivered() {
		switch e.Type {
		case eventbus.CustomerMessageReceived:
			gotReceived = true
		case eventbus.CustomerMessageReplied:
			gotReplied = true
		}
	}
	if !gotReceived {
		t.Fatalf("missing CustomerMessageReceived")
	}
	if !gotReplied {
		t.Fatalf("missing CustomerMessageReplied")
	}
}

func TestMessageWebhook_EscalatesOnLowConfidence(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	// Classifier returns a low-confidence general_enquiry shape.
	pipeline, _, _ := buildPipeline(t, classifierLLMResponse(customerservice.IntentGeneralEnquiry, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.32), nil, sender, "tiktok", bus)
	handler, verifier := buildTikTokMessageHandler(t, pipeline, &recordingMessagingMetrics{})

	body := tikTokMessageBody(t, "m-low")
	req := signedTikTokMessageRequest(t, body, verifier)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d", rec.Code)
	}
	if sender.calls.Load() != 0 {
		t.Fatalf("sender calls = %d, want 0 (escalated)", sender.calls.Load())
	}
	gotEscalated := false
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.CustomerMessageEscalatedToOperator {
			gotEscalated = true
		}
	}
	if !gotEscalated {
		t.Fatalf("missing CustomerMessageEscalatedToOperator")
	}
}

func TestTikTokMessageWebhook_ChannelSendFailureEscalates(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{err: errors.New("tiktok api 503")}
	pipeline, _, _ := buildPipeline(t, classifierLLMResponse(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.92), nil, sender, "tiktok", bus)
	handler, verifier := buildTikTokMessageHandler(t, pipeline, &recordingMessagingMetrics{})

	body := tikTokMessageBody(t, "m-send-fail")
	req := signedTikTokMessageRequest(t, body, verifier)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500 (channel send failed)", rec.Code)
	}
	gotEscalated := false
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.CustomerMessageEscalatedToOperator {
			gotEscalated = true
		}
	}
	if !gotEscalated {
		t.Fatalf("missing CustomerMessageEscalatedToOperator (send failure escalates)")
	}
}

// Facebook tests
func buildFacebookMessageHandler(t *testing.T, pipeline *MessagingPipeline, metrics *recordingMessagingMetrics) *FacebookMessageHandler {
	t.Helper()
	handler, err := NewFacebookMessageHandler(nil, FacebookMessageHandlerConfig{
		AppSecret:   []byte(testFacebookAppSecret),
		Pipeline:    pipeline,
		Idempotency: NewMemoryIdempotencyStore(),
		TenantID:    "tenant-cs",
		Channel:     "facebook",
		Metrics:     metrics,
		Now:         func() time.Time { return fixedMessagingTime },
	})
	if err != nil {
		t.Fatalf("NewFacebookMessageHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close(context.Background()) })
	return handler
}

func facebookMessageBody(t *testing.T, mid string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"object": "page",
		"entry": []map[string]any{
			{
				"id":   "page-1",
				"time": fixedMessagingTime.UnixMilli(),
				"messaging": []map[string]any{
					{
						"sender":    map[string]string{"id": "psid-789"},
						"recipient": map[string]string{"id": "page-1"},
						"timestamp": fixedMessagingTime.UnixMilli(),
						"message": map[string]string{
							"mid":  mid,
							"text": "How long does shipping to Sydney take?",
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return body
}

func signedFacebookMessageRequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	header, err := social.SignFacebookWebhook([]byte(testFacebookAppSecret), body)
	if err != nil {
		t.Fatalf("SignFacebookWebhook: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/facebook/messages", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", header)
	return req
}

func TestFacebookMessageWebhook_VerifiesXHubSignature(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	pipeline, _, _ := buildPipeline(t, classifierLLMResponse(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.92), nil, sender, "facebook", bus)
	handler := buildFacebookMessageHandler(t, pipeline, &recordingMessagingMetrics{})

	body := facebookMessageBody(t, "fb-mid-1")
	req := httptest.NewRequest(http.MethodPost, "/webhooks/facebook/messages", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", "sha256=deadbeef")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (bad signature)", rec.Code)
	}

	// Now retry with a valid signature.
	req2 := signedFacebookMessageRequest(t, body)
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("valid status = %d, want 200", rec2.Code)
	}
	if sender.calls.Load() != 1 {
		t.Fatalf("sender calls = %d, want 1 (FB end-to-end)", sender.calls.Load())
	}
}

func TestFacebookMessageWebhook_MalformedEnvelope(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	pipeline, _, _ := buildPipeline(t, "anything", nil, sender, "facebook", bus)
	handler := buildFacebookMessageHandler(t, pipeline, &recordingMessagingMetrics{})

	body := []byte(`{"object":"page","entry":[]}`)
	req := signedFacebookMessageRequest(t, body)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (no message)", rec.Code)
	}
}

func TestNewTikTokMessageHandler_ConfigValidation(t *testing.T) {
	t.Parallel()
	verifier, _ := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{Secret: []byte(testMessageWebhookSecret)})
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	pipeline, _, _ := buildPipeline(t, "x", nil, sender, "tiktok", bus)
	store := NewMemoryIdempotencyStore()
	cases := map[string]TikTokMessageHandlerConfig{
		"missing_verifier":    {Pipeline: pipeline, Idempotency: store, TenantID: "t"},
		"missing_pipeline":    {Verifier: verifier, Idempotency: store, TenantID: "t"},
		"missing_idempotency": {Verifier: verifier, Pipeline: pipeline, TenantID: "t"},
		"missing_tenant":      {Verifier: verifier, Pipeline: pipeline, Idempotency: store},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewTikTokMessageHandler(nil, cfg)
			if !errors.Is(err, ErrWebhookUnconfigured) {
				t.Fatalf("err = %v, want ErrWebhookUnconfigured", err)
			}
		})
	}
}

func TestNewFacebookMessageHandler_ConfigValidation(t *testing.T) {
	t.Parallel()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &recordingSender{}
	pipeline, _, _ := buildPipeline(t, "x", nil, sender, "facebook", bus)
	store := NewMemoryIdempotencyStore()
	cases := map[string]FacebookMessageHandlerConfig{
		"missing_secret":      {Pipeline: pipeline, Idempotency: store, TenantID: "t"},
		"missing_pipeline":    {AppSecret: []byte("s"), Idempotency: store, TenantID: "t"},
		"missing_idempotency": {AppSecret: []byte("s"), Pipeline: pipeline, TenantID: "t"},
		"missing_tenant":      {AppSecret: []byte("s"), Pipeline: pipeline, Idempotency: store},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewFacebookMessageHandler(nil, cfg)
			if !errors.Is(err, ErrWebhookUnconfigured) {
				t.Fatalf("err = %v, want ErrWebhookUnconfigured", err)
			}
		})
	}
}
