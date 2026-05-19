//go:build v361_smoke

// File scope: v3.6.1 QA Task 2 -- inbound->reply E2E within 30s
// (EC-8-3 hardening).
//
// Acceptance (cite plan + EC-8-3 hardening): "TikTok + Facebook
// inbound webhook full pipeline through to outbound reply OR
// operator escalation; end-to-end latency <=30s; correct reply
// text; SendMessage invoked exactly once on success; audit log
// entry persisted with full pipeline trace; ec_message_webhook_*
// + ec_faq_responses_total incremented correctly".
//
// 8 E2E scenarios (full pipeline driven through the real
// EnquiryClassifier + FAQResponder + MessagingPipeline + per-
// channel webhook handler; the LLM and channel adapter are
// stubbed so the suite stays hermetic):
//
//  1. TikTok high-confidence FAQ match  -> auto-reply within 5s
//  2. Facebook high-confidence FAQ match -> auto-reply within 5s
//  3. Medium-confidence                 -> suggested-reply queued
//  4. Low-confidence                    -> escalation event
//  5. Negative sentiment + complaint    -> urgent flag + operator
//  6. zh-cn refund                      -> handled correctly
//  7. Idempotent retry                  -> cached reply, no double-send
//  8. LLM unavailable                   -> rule fallback + template
//
// The suite drives the production composition shape:
//
//	httptest.NewServer(handler)
//	  -> handler -> verifySignature -> reserveDup
//	     -> MessagingPipeline.Process
//	        -> classifier.Classify (LLM stub or rule fallback)
//	        -> responder.Respond   (LLM stub or template fallback)
//	        -> sender.SendMessage  (recordingSender for audit log)
//	        -> publisher.Publish   (eventbus.InMemoryBus)
//	        -> metrics.RecordMessageWebhook + RecordFAQResponse
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4):
//   - top-level test stays a thin orchestrator that delegates to
//     per-scenario helpers
//   - LLM stub, FAQ store, sender, harness factory all split into
//     focused functions below.
package v361

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/social"
	"github.com/nfsarch33/helixon-ec/internal/agent/customerservice"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/metrics"
	"github.com/nfsarch33/helixon-ec/internal/observability"
	"github.com/nfsarch33/helixon-ec/internal/port"
	"github.com/nfsarch33/helixon-ec/internal/webhook"
)

// e2eDeadline is the per-scenario end-to-end latency budget per
// the EC-8-3 acceptance ("inbound webhook -> classifier -> outbound
// reply within 30s end-to-end"). The handler runs in-process so
// real wall-clock typically lands sub-millisecond; the ceiling is
// the production budget the pipeline commits to.
const e2eDeadline = 30 * time.Second

// e2eFastReplyDeadline is the tighter sub-budget for high-confidence
// auto-reply scenarios (1, 2) and urgent operator notification
// (5). Plan: "auto-reply within 5s" + "operator notified within 5s".
const e2eFastReplyDeadline = 5 * time.Second

// e2eMessagingSecret is the deterministic webhook secret for
// scenarios 1, 6, 7, 8 (TikTok). gitleaks:allow
const e2eMessagingSecret = "v361-tiktok-webhook-test-secret-fixture-bytes" // gitleaks:allow

// e2eFacebookAppSecret is the deterministic Facebook app secret
// for scenarios 2 and 4. gitleaks:allow
const e2eFacebookAppSecret = "v361-facebook-app-secret-test-fixture-bytes-32" // gitleaks:allow

// fixedE2ETime keeps deterministic timestamps in fixtures.
var fixedE2ETime = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

// scenarioOutcome captures the routed destination + measured
// latency for one E2E scenario. Used for the per-scenario t.Log
// emit + the artifact table the PR body cites.
type scenarioOutcome struct {
	name      string
	channel   string
	latency   time.Duration
	httpCode  int
	autoReply bool
	escalated bool
	urgent    bool
	replyText string
}

// e2eScenarioStubLLM is the per-scenario LLM stub. Some scenarios
// want the LLM to succeed (high-confidence path); others want it
// to fail (rule fallback path). The flag captures both modes.
type e2eScenarioStubLLM struct {
	classifierResp port.AICompletionResponse
	classifierErr  error
	rephraseResp   port.AICompletionResponse
	rephraseErr    error
	calls          atomic.Int32
}

// Complete returns the configured response based on call count.
// First call (classifier) returns classifierResp/Err; subsequent
// calls (rephrase) return rephraseResp/Err. Mirrors the
// classifier->responder ordering in MessagingPipeline.Process.
func (s *e2eScenarioStubLLM) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	idx := s.calls.Add(1)
	if idx == 1 {
		return s.classifierResp, s.classifierErr
	}
	return s.rephraseResp, s.rephraseErr
}

// e2eRecordingSender is the in-test port.OutboundMessageSender
// that records every Send call. Per-channel; the sender count is
// the audit log the assertion checks.
type e2eRecordingSender struct {
	calls atomic.Int32
	last  port.OutboundMessageRequest
	err   error
}

// SendMessage satisfies port.OutboundMessageSender.
func (s *e2eRecordingSender) SendMessage(_ context.Context, req port.OutboundMessageRequest) (port.OutboundMessageResponse, error) {
	s.calls.Add(1)
	s.last = req
	if s.err != nil {
		return port.OutboundMessageResponse{}, s.err
	}
	return port.OutboundMessageResponse{ProviderMessageID: "provider-" + req.ThreadID}, nil
}

// e2eFAQStore is the in-test FAQ store. Scenarios 1, 2, 6 wire
// it with matching entries; scenarios 4, 8 leave it empty so the
// no-match escalation path fires.
type e2eFAQStore struct {
	entries []customerservice.FAQEntry
}

// Search satisfies customerservice.FAQStore.
func (s *e2eFAQStore) Search(_ context.Context, query customerservice.FAQSearchQuery) ([]customerservice.FAQEntry, error) {
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

// e2eHarness bundles every wired component for one scenario.
type e2eHarness struct {
	bus       *eventbus.InMemoryBus
	sender    *e2eRecordingSender
	llm       *e2eScenarioStubLLM
	pipeline  *webhook.MessagingPipeline
	registry  *metrics.Registry
	v360      *observability.V360Metrics
	tenantID  string
	storeSize int
}

// e2eHarnessConfig is the per-scenario harness wiring envelope.
type e2eHarnessConfig struct {
	tenantID       string
	channel        string
	classifierResp port.AICompletionResponse
	classifierErr  error
	rephraseResp   port.AICompletionResponse
	rephraseErr    error
	senderErr      error
	faqEntries     []customerservice.FAQEntry
}

// setupE2EHarness wires the EC-8-1 classifier + EC-8-2 responder
// + EC-8-3 pipeline + metric registry under one harness. Every
// component registers with t.Cleanup so each scenario gets a
// hermetic surface.
func setupE2EHarness(t *testing.T, cfg e2eHarnessConfig) *e2eHarness {
	t.Helper()
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })
	sender := &e2eRecordingSender{err: cfg.senderErr}
	llm := &e2eScenarioStubLLM{
		classifierResp: cfg.classifierResp,
		classifierErr:  cfg.classifierErr,
		rephraseResp:   cfg.rephraseResp,
		rephraseErr:    cfg.rephraseErr,
	}
	registry := metrics.NewRegistry("v361-smoke")
	v360 := observability.NewV360Metrics(registry)
	classifier, err := customerservice.NewEnquiryClassifier(nil, customerservice.EnquiryClassifierConfig{
		Generator: llm,
		TenantID:  cfg.tenantID,
		Now:       func() time.Time { return fixedE2ETime },
		Metrics:   v360,
	})
	if err != nil {
		t.Fatalf("NewEnquiryClassifier: %v", err)
	}
	t.Cleanup(func() { _ = classifier.Close(context.Background()) })
	responder, err := customerservice.NewFAQResponder(nil, customerservice.FAQResponderConfig{
		Generator: llm,
		Store:     &e2eFAQStore{entries: cfg.faqEntries},
		TenantID:  cfg.tenantID,
		Now:       func() time.Time { return fixedE2ETime },
		Metrics:   v360,
	})
	if err != nil {
		t.Fatalf("NewFAQResponder: %v", err)
	}
	t.Cleanup(func() { _ = responder.Close(context.Background()) })
	pipeline, err := webhook.NewMessagingPipeline(nil, webhook.MessagingPipelineConfig{
		Classifier: classifier,
		Responder:  responder,
		Senders:    map[string]port.OutboundMessageSender{cfg.channel: sender},
		Publisher:  bus,
		Now:        func() time.Time { return fixedE2ETime },
		Metrics:    v360,
	})
	if err != nil {
		t.Fatalf("NewMessagingPipeline: %v", err)
	}
	t.Cleanup(func() { _ = pipeline.Close(context.Background()) })
	return &e2eHarness{
		bus:       bus,
		sender:    sender,
		llm:       llm,
		pipeline:  pipeline,
		registry:  registry,
		v360:      v360,
		tenantID:  cfg.tenantID,
		storeSize: len(cfg.faqEntries),
	}
}

// e2eFAQEntries returns the curated FAQ entries used across
// auto-reply scenarios (1, 2, 5, 6). Sized to exercise the
// rerank top-3 cap.
func e2eFAQEntries(tenantID string) []customerservice.FAQEntry {
	return []customerservice.FAQEntry{
		{
			TenantID: tenantID, EntryID: "faq-shipping-sydney",
			Language: customerservice.LanguageEN, IntentCategory: customerservice.IntentShippingQuery,
			Question: "How long does shipping to Sydney take?",
			Answer:   "Sydney metro deliveries arrive in 3-5 business days for standard shipping.",
			Keywords: []string{"shipping", "sydney", "delivery", "ship"},
		},
		{
			TenantID: tenantID, EntryID: "faq-refund-policy-en",
			Language: customerservice.LanguageEN, IntentCategory: customerservice.IntentRefundRequest,
			Question: "What is the refund policy?",
			Answer:   "Refunds are processed within 7 business days of approval.",
			Keywords: []string{"refund", "policy"},
		},
		{
			TenantID: tenantID, EntryID: "faq-refund-policy-cn",
			Language: customerservice.LanguageZHCN, IntentCategory: customerservice.IntentRefundRequest,
			Question: "退款多久到账？",
			Answer:   "退款将在批准后 7 个工作日内处理完成。",
			Keywords: []string{"退款", "退货"},
		},
		{
			TenantID: tenantID, EntryID: "faq-complaint-handling",
			Language: customerservice.LanguageEN, IntentCategory: customerservice.IntentComplaint,
			Question: "How is a complaint handled?",
			Answer:   "We will escalate this to a senior representative within 24 hours.",
			Keywords: []string{"complaint", "issue"},
		},
	}
}

// classifierJSON returns the deterministic LLM classifier reply.
func classifierJSON(intent customerservice.Intent, sentiment customerservice.Sentiment, lang customerservice.Language, conf float64) port.AICompletionResponse {
	body, _ := json.Marshal(map[string]any{
		"intent":     string(intent),
		"sentiment":  string(sentiment),
		"language":   string(lang),
		"confidence": conf,
	})
	return port.AICompletionResponse{Content: string(body)}
}

// rephraseReply returns the deterministic LLM rephrase reply.
func rephraseReply(text string) port.AICompletionResponse {
	return port.AICompletionResponse{Content: text}
}

// servingTikTokHandler wires the EC-8-3 TikTok handler around the
// supplied pipeline + metrics + idempotency surface, then
// returns an http.Handler ready for httptest.
func servingTikTokHandler(t *testing.T, h *e2eHarness, idem webhook.IdempotencyStore) (*webhook.TikTokMessageHandler, *social.TikTokWebhookVerifier) {
	t.Helper()
	verifier, err := social.NewTikTokWebhookVerifier(social.TikTokWebhookConfig{
		Secret: []byte(e2eMessagingSecret),
		Now:    func() time.Time { return fixedE2ETime },
	})
	if err != nil {
		t.Fatalf("NewTikTokWebhookVerifier: %v", err)
	}
	handler, err := webhook.NewTikTokMessageHandler(nil, webhook.TikTokMessageHandlerConfig{
		Verifier:    verifier,
		Pipeline:    h.pipeline,
		Idempotency: idem,
		TenantID:    h.tenantID,
		Channel:     "tiktok",
		Metrics:     h.v360,
		Now:         func() time.Time { return fixedE2ETime },
	})
	if err != nil {
		t.Fatalf("NewTikTokMessageHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close(context.Background()) })
	return handler, verifier
}

// servingFacebookHandler wires the EC-8-3 Facebook handler.
func servingFacebookHandler(t *testing.T, h *e2eHarness, idem webhook.IdempotencyStore) *webhook.FacebookMessageHandler {
	t.Helper()
	handler, err := webhook.NewFacebookMessageHandler(nil, webhook.FacebookMessageHandlerConfig{
		AppSecret:   []byte(e2eFacebookAppSecret),
		Pipeline:    h.pipeline,
		Idempotency: idem,
		TenantID:    h.tenantID,
		Channel:     "facebook",
		Metrics:     h.v360,
		Now:         func() time.Time { return fixedE2ETime },
	})
	if err != nil {
		t.Fatalf("NewFacebookMessageHandler: %v", err)
	}
	t.Cleanup(func() { _ = handler.Close(context.Background()) })
	return handler
}

// e2eTikTokRequestBody builds a canonical TikTok message body for
// the given message id + text.
func e2eTikTokRequestBody(t *testing.T, tenantID, messageID, text string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"tenant_id":   tenantID,
		"message_id":  messageID,
		"thread_id":   "thread-" + messageID,
		"buyer_id":    "buyer-" + messageID,
		"text":        text,
		"occurred_at": fixedE2ETime.Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("marshal TikTok body: %v", err)
	}
	return body
}

// e2eFacebookRequestBody builds a canonical Facebook nested-shape
// message body.
func e2eFacebookRequestBody(t *testing.T, tenantID, mid, text string) []byte {
	t.Helper()
	body, err := json.Marshal(map[string]any{
		"tenant_id": tenantID,
		"object":    "page",
		"entry": []map[string]any{
			{
				"id":   "page-1",
				"time": fixedE2ETime.UnixMilli(),
				"messaging": []map[string]any{
					{
						"sender":    map[string]string{"id": "psid-" + mid},
						"recipient": map[string]string{"id": "page-1"},
						"timestamp": fixedE2ETime.UnixMilli(),
						"message": map[string]string{
							"mid":  mid,
							"text": text,
						},
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("marshal Facebook body: %v", err)
	}
	return body
}

// signedTikTokE2ERequest builds a properly-signed TikTok webhook
// http.Request for the supplied body.
func signedTikTokE2ERequest(t *testing.T, body []byte, verifier *social.TikTokWebhookVerifier) *http.Request {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/webhooks/tiktok/messages", bytes.NewReader(body))
	req.Header.Set("X-Tts-Signature", verifier.SignWebhook(fixedE2ETime.Add(-30*time.Second).Unix(), body))
	return req
}

// signedFacebookE2ERequest builds a properly-signed Facebook
// webhook http.Request for the supplied body.
func signedFacebookE2ERequest(t *testing.T, body []byte) *http.Request {
	t.Helper()
	header, err := social.SignFacebookWebhook([]byte(e2eFacebookAppSecret), body)
	if err != nil {
		t.Fatalf("SignFacebookWebhook: %v", err)
	}
	req := httptest.NewRequest(http.MethodPost, "/webhooks/facebook/messages", bytes.NewReader(body))
	req.Header.Set("X-Hub-Signature-256", header)
	return req
}

// runTikTokE2E pushes one signed request through the TikTok
// handler + asserts <30s end-to-end. Returns the recorder so
// per-scenario assertions can pivot on the response.
func runTikTokE2E(t *testing.T, h *e2eHarness, idem webhook.IdempotencyStore, body []byte) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	handler, verifier := servingTikTokHandler(t, h, idem)
	req := signedTikTokE2ERequest(t, body, verifier)
	rec := httptest.NewRecorder()
	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)
	if elapsed > e2eDeadline {
		t.Fatalf("E2E elapsed %s exceeds %s budget", elapsed, e2eDeadline)
	}
	return rec, elapsed
}

// runFacebookE2E is the Facebook counterpart.
func runFacebookE2E(t *testing.T, h *e2eHarness, idem webhook.IdempotencyStore, body []byte) (*httptest.ResponseRecorder, time.Duration) {
	t.Helper()
	handler := servingFacebookHandler(t, h, idem)
	req := signedFacebookE2ERequest(t, body)
	rec := httptest.NewRecorder()
	start := time.Now()
	handler.ServeHTTP(rec, req)
	elapsed := time.Since(start)
	if elapsed > e2eDeadline {
		t.Fatalf("E2E elapsed %s exceeds %s budget", elapsed, e2eDeadline)
	}
	return rec, elapsed
}

// busHasReplied returns true when at least one CustomerMessageReplied
// is present in the bus.
func busHasReplied(bus *eventbus.InMemoryBus) bool {
	return countEventsOfType(bus, eventbus.CustomerMessageReplied) > 0
}

// countEventsOfType returns the number of events of the given
// type the bus has dispatched.
func countEventsOfType(bus *eventbus.InMemoryBus, t eventbus.EventType) int {
	count := 0
	for _, e := range bus.Delivered() {
		if e.Type == t {
			count++
		}
	}
	return count
}

// busHasEscalated returns true when a CustomerMessageEscalatedToOperator
// event is present in the bus.
func busHasEscalated(bus *eventbus.InMemoryBus) bool {
	return countEventsOfType(bus, eventbus.CustomerMessageEscalatedToOperator) > 0
}

// firstReplyEvent returns the first CustomerMessageReplied event
// (or zero-value if none).
func firstReplyEvent(bus *eventbus.InMemoryBus) eventbus.Event {
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.CustomerMessageReplied {
			return e
		}
	}
	return eventbus.Event{}
}

// firstEscalatedEvent returns the first escalation event.
func firstEscalatedEvent(bus *eventbus.InMemoryBus) eventbus.Event {
	for _, e := range bus.Delivered() {
		if e.Type == eventbus.CustomerMessageEscalatedToOperator {
			return e
		}
	}
	return eventbus.Event{}
}

// assertWebhookMetricIncrement verifies the
// ec_message_webhook_received_total counter incremented for the
// expected status. Reads the registry's exposition output so the
// assertion mirrors what Prometheus would scrape.
func assertWebhookMetricIncrement(t *testing.T, h *e2eHarness, channel, status string, want int) {
	t.Helper()
	exposition := scrapeRegistry(t, h.registry)
	needle := fmt.Sprintf(`ec_message_webhook_received_total{binary="v361-smoke",channel=%q,status=%q,tenant_id=%q} %d`, channel, status, h.tenantID, want)
	if !strings.Contains(exposition, needle) {
		t.Fatalf("metric not found:\nwant: %s\nfull exposition:\n%s", needle, exposition)
	}
}

// assertFAQResponseMetric verifies the ec_faq_responses_total
// counter incremented for the expected outcome.
func assertFAQResponseMetric(t *testing.T, h *e2eHarness, outcome string, want int) {
	t.Helper()
	exposition := scrapeRegistry(t, h.registry)
	needle := fmt.Sprintf(`ec_faq_responses_total{binary="v361-smoke",outcome=%q,tenant_id=%q} %d`, outcome, h.tenantID, want)
	if !strings.Contains(exposition, needle) {
		t.Fatalf("metric not found:\nwant: %s\nfull exposition:\n%s", needle, exposition)
	}
}

// scrapeRegistry calls the registry's /metrics handler and returns
// the body. Cheap helper so per-scenario assertions don't repeat.
func scrapeRegistry(t *testing.T, registry *metrics.Registry) string {
	t.Helper()
	srv := httptest.NewServer(registry.Handler())
	t.Cleanup(srv.Close)
	resp, err := srv.Client().Get(srv.URL)
	if err != nil {
		t.Fatalf("scrape registry: %v", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("scrape body: %v", err)
	}
	return string(body)
}

// e2eOutcomeRecorder accumulates per-scenario outcomes for the
// summary t.Log emit + the artifact table. Safe for concurrent
// use because every scenario subtest runs in t.Parallel.
type e2eOutcomeRecorder struct {
	mu   sync.Mutex
	rows []scenarioOutcome
}

func (r *e2eOutcomeRecorder) record(o scenarioOutcome) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.rows = append(r.rows, o)
}

func (r *e2eOutcomeRecorder) summary() string {
	r.mu.Lock()
	rows := make([]scenarioOutcome, len(r.rows))
	copy(rows, r.rows)
	r.mu.Unlock()
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].name < rows[j].name })
	var sb strings.Builder
	sb.WriteString("v3.6.1 E2E scenario summary (8 rows)\n")
	for _, row := range rows {
		fmt.Fprintf(&sb, "  %-44s channel=%s http=%d latency=%s autoReply=%t escalated=%t urgent=%t\n",
			row.name, row.channel, row.httpCode, row.latency, row.autoReply, row.escalated, row.urgent)
	}
	return sb.String()
}

// TestMessagingE2E_Scenarios runs all 8 EC-8-3 hardening scenarios
// against the production-shape pipeline + asserts the per-scenario
// acceptance criteria.
func TestMessagingE2E_Scenarios(t *testing.T) {
	t.Parallel()
	recorder := &e2eOutcomeRecorder{}
	t.Run("scenario_1_tiktok_high_confidence_auto_reply", func(t *testing.T) {
		t.Parallel()
		runScenario1HighConfidenceTikTok(t, recorder)
	})
	t.Run("scenario_2_facebook_high_confidence_auto_reply", func(t *testing.T) {
		t.Parallel()
		runScenario2HighConfidenceFacebook(t, recorder)
	})
	t.Run("scenario_3_medium_confidence_suggested", func(t *testing.T) {
		t.Parallel()
		runScenario3MediumConfidence(t, recorder)
	})
	t.Run("scenario_4_low_confidence_escalated", func(t *testing.T) {
		t.Parallel()
		runScenario4LowConfidence(t, recorder)
	})
	t.Run("scenario_5_negative_urgent_complaint", func(t *testing.T) {
		t.Parallel()
		runScenario5NegativeUrgentComplaint(t, recorder)
	})
	t.Run("scenario_6_zh_cn_refund", func(t *testing.T) {
		t.Parallel()
		runScenario6ChineseRefund(t, recorder)
	})
	t.Run("scenario_7_idempotent_retry", func(t *testing.T) {
		t.Parallel()
		runScenario7IdempotentRetry(t, recorder)
	})
	t.Run("scenario_8_llm_unavailable_rule_fallback", func(t *testing.T) {
		t.Parallel()
		runScenario8LLMUnavailable(t, recorder)
	})
	t.Cleanup(func() { t.Log(recorder.summary()) })
}

// runScenario1HighConfidenceTikTok proves a TikTok shipping query
// matches an FAQ at 0.92 classifier confidence -> auto-reply within
// 5s + sender called once + replied event emitted + metric
// incremented.
func runScenario1HighConfidenceTikTok(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-1"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "tiktok",
		classifierResp: classifierJSON(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.92),
		rephraseResp:   rephraseReply("Sydney metro deliveries arrive in 3-5 business days. Thanks!"),
		faqEntries:     e2eFAQEntries(tenantID),
	})
	body := e2eTikTokRequestBody(t, tenantID, "m-s1", "How long does shipping to Sydney take?")
	resp, latency := runTikTokE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if latency > e2eFastReplyDeadline {
		t.Fatalf("latency = %s, want <= %s (high-confidence FAQ)", latency, e2eFastReplyDeadline)
	}
	if h.sender.calls.Load() != 1 {
		t.Fatalf("sender calls = %d, want 1", h.sender.calls.Load())
	}
	if !busHasReplied(h.bus) {
		t.Fatalf("missing CustomerMessageReplied event")
	}
	if got := h.sender.last.Text; !strings.Contains(got, "3-5 business days") {
		t.Fatalf("reply text = %q, want LLM-rephrased entry containing '3-5 business days'", got)
	}
	assertWebhookMetricIncrement(t, h, "tiktok", "replied", 1)
	assertFAQResponseMetric(t, h, "auto_replied", 1)
	rec.record(scenarioOutcome{name: "1_tiktok_high_confidence", channel: "tiktok", httpCode: resp.Code, latency: latency, autoReply: true, replyText: h.sender.last.Text})
}

// runScenario2HighConfidenceFacebook is the Facebook counterpart.
func runScenario2HighConfidenceFacebook(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-2"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "facebook",
		classifierResp: classifierJSON(customerservice.IntentRefundRequest, customerservice.SentimentNegative, customerservice.LanguageEN, 0.91),
		rephraseResp:   rephraseReply("Refunds are processed within 7 business days of approval."),
		faqEntries:     e2eFAQEntries(tenantID),
	})
	body := e2eFacebookRequestBody(t, tenantID, "fb-s2", "What is the refund policy please?")
	resp, latency := runFacebookE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if latency > e2eFastReplyDeadline {
		t.Fatalf("latency = %s, want <= %s (high-confidence FAQ FB)", latency, e2eFastReplyDeadline)
	}
	if h.sender.calls.Load() != 1 {
		t.Fatalf("sender calls = %d, want 1", h.sender.calls.Load())
	}
	if !busHasReplied(h.bus) {
		t.Fatalf("missing CustomerMessageReplied event")
	}
	if got := h.sender.last.Text; !strings.Contains(got, "7 business days") {
		t.Fatalf("reply text = %q, want '7 business days'", got)
	}
	assertWebhookMetricIncrement(t, h, "facebook", "replied", 1)
	assertFAQResponseMetric(t, h, "auto_replied", 1)
	rec.record(scenarioOutcome{name: "2_facebook_high_confidence", channel: "facebook", httpCode: resp.Code, latency: latency, autoReply: true, replyText: h.sender.last.Text})
}

// runScenario3MediumConfidence proves the suggested-reply gate
// fires when confidence sits between 0.6 and 0.8 -> queued for
// operator review (no auto-send).
func runScenario3MediumConfidence(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-3"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "tiktok",
		classifierResp: classifierJSON(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.7),
		rephraseResp:   rephraseReply("Sydney metro deliveries arrive in 3-5 business days."),
		faqEntries:     e2eFAQEntries(tenantID),
	})
	body := e2eTikTokRequestBody(t, tenantID, "m-s3", "Hi quick query about shipping to Sydney metro.")
	resp, latency := runTikTokE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if h.sender.calls.Load() != 0 {
		t.Fatalf("sender calls = %d, want 0 (suggested -> no auto-send)", h.sender.calls.Load())
	}
	if !busHasEscalated(h.bus) {
		t.Fatalf("missing escalation event for medium-confidence suggested path")
	}
	evt := firstEscalatedEvent(h.bus)
	if got, _ := evt.Payload["outcome"].(string); got != "suggested" {
		t.Fatalf("escalation payload outcome = %q, want suggested", got)
	}
	assertWebhookMetricIncrement(t, h, "tiktok", "suggested", 1)
	assertFAQResponseMetric(t, h, "suggested", 1)
	rec.record(scenarioOutcome{name: "3_tiktok_medium_suggested", channel: "tiktok", httpCode: resp.Code, latency: latency, escalated: true})
}

// runScenario4LowConfidence proves the low-confidence path
// (FlagForReview triggered) escalates to operator immediately.
func runScenario4LowConfidence(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-4"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "facebook",
		classifierResp: classifierJSON(customerservice.IntentGeneralEnquiry, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.32),
		rephraseResp:   rephraseReply(""),
		faqEntries:     []customerservice.FAQEntry{},
	})
	body := e2eFacebookRequestBody(t, tenantID, "fb-s4", "Hello question please ambiguous garble.")
	resp, latency := runFacebookE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if h.sender.calls.Load() != 0 {
		t.Fatalf("sender calls = %d, want 0 (low confidence -> escalate)", h.sender.calls.Load())
	}
	if !busHasEscalated(h.bus) {
		t.Fatalf("missing escalation event")
	}
	evt := firstEscalatedEvent(h.bus)
	if got, _ := evt.Payload["reason"].(string); got != "low_confidence" && got != "no_faq_match" {
		t.Fatalf("escalation reason = %q, want low_confidence or no_faq_match", got)
	}
	assertWebhookMetricIncrement(t, h, "facebook", "escalated", 1)
	assertFAQResponseMetric(t, h, "escalated", 1)
	rec.record(scenarioOutcome{name: "4_facebook_low_escalated", channel: "facebook", httpCode: resp.Code, latency: latency, escalated: true})
}

// runScenario5NegativeUrgentComplaint proves negative-sentiment
// urgent complaints route through the operator escalation path
// within the 5s urgent operator-notification budget. The text
// contains an urgency marker so mergeSentiment promotes to urgent.
func runScenario5NegativeUrgentComplaint(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-5"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "tiktok",
		classifierResp: classifierJSON(customerservice.IntentComplaint, customerservice.SentimentNegative, customerservice.LanguageEN, 0.55),
		rephraseResp:   rephraseReply(""),
		faqEntries:     e2eFAQEntries(tenantID),
	})
	body := e2eTikTokRequestBody(t, tenantID, "m-s5", "URGENT! my package is broke and you ignored my email!")
	resp, latency := runTikTokE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if latency > e2eFastReplyDeadline {
		t.Fatalf("latency = %s, want <= %s (urgent operator notify budget)", latency, e2eFastReplyDeadline)
	}
	if h.sender.calls.Load() != 0 {
		t.Fatalf("sender calls = %d, want 0 (urgent complaint -> escalate)", h.sender.calls.Load())
	}
	if !busHasEscalated(h.bus) {
		t.Fatalf("missing escalation event for urgent complaint")
	}
	evt := firstEscalatedEvent(h.bus)
	if got, _ := evt.Payload["sentiment"].(string); got != string(customerservice.SentimentUrgent) {
		t.Fatalf("escalation sentiment = %q, want urgent", got)
	}
	assertWebhookMetricIncrement(t, h, "tiktok", "escalated", 1)
	rec.record(scenarioOutcome{name: "5_tiktok_negative_urgent", channel: "tiktok", httpCode: resp.Code, latency: latency, escalated: true, urgent: true})
}

// runScenario6ChineseRefund proves the bilingual zh-cn refund
// path: classifier returns zh-cn refund + responder returns the
// CN FAQ + auto-reply.
func runScenario6ChineseRefund(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-6"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "tiktok",
		classifierResp: classifierJSON(customerservice.IntentRefundRequest, customerservice.SentimentNegative, customerservice.LanguageZHCN, 0.93),
		rephraseResp:   rephraseReply("您好，退款将在 7 个工作日内处理完成。"),
		faqEntries:     e2eFAQEntries(tenantID),
	})
	body := e2eTikTokRequestBody(t, tenantID, "m-s6", "你好，我想申请退款，多久能到账？")
	resp, latency := runTikTokE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.Code)
	}
	if h.sender.calls.Load() != 1 {
		t.Fatalf("sender calls = %d, want 1 (zh-cn auto-reply)", h.sender.calls.Load())
	}
	if got := h.sender.last.Text; !strings.Contains(got, "退款") {
		t.Fatalf("reply text = %q, want CN refund response", got)
	}
	if !busHasReplied(h.bus) {
		t.Fatalf("missing CustomerMessageReplied event for zh-cn")
	}
	evt := firstReplyEvent(h.bus)
	if got, _ := evt.Payload["language"].(string); got != string(customerservice.LanguageZHCN) {
		t.Fatalf("reply payload language = %q, want zh-cn", got)
	}
	rec.record(scenarioOutcome{name: "6_tiktok_zh_cn_refund", channel: "tiktok", httpCode: resp.Code, latency: latency, autoReply: true, replyText: h.sender.last.Text})
}

// runScenario7IdempotentRetry proves three identical webhook
// deliveries on the same message_id only fire SendMessage once.
func runScenario7IdempotentRetry(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-7"
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:       tenantID,
		channel:        "tiktok",
		classifierResp: classifierJSON(customerservice.IntentShippingQuery, customerservice.SentimentNeutral, customerservice.LanguageEN, 0.92),
		rephraseResp:   rephraseReply("Sydney metro deliveries arrive in 3-5 business days."),
		faqEntries:     e2eFAQEntries(tenantID),
	})
	idem := webhook.NewMemoryIdempotencyStore()
	body := e2eTikTokRequestBody(t, tenantID, "m-s7-dup", "How long does shipping to Sydney take?")
	handler, verifier := servingTikTokHandler(t, h, idem)
	for i := 0; i < 3; i++ {
		req := signedTikTokE2ERequest(t, body, verifier)
		rec2 := httptest.NewRecorder()
		handler.ServeHTTP(rec2, req)
		if rec2.Code != http.StatusOK {
			t.Fatalf("attempt %d status = %d", i, rec2.Code)
		}
	}
	if h.sender.calls.Load() != 1 {
		t.Fatalf("sender calls = %d, want 1 (idempotent retry must NOT double-send)", h.sender.calls.Load())
	}
	if got := countEventsOfType(h.bus, eventbus.CustomerMessageReceived); got != 1 {
		t.Fatalf("CustomerMessageReceived count = %d, want 1 (idempotent)", got)
	}
	rec.record(scenarioOutcome{name: "7_tiktok_idempotent_retry", channel: "tiktok", httpCode: http.StatusOK, latency: 0, autoReply: true, replyText: h.sender.last.Text})
}

// runScenario8LLMUnavailable proves the LLM-unavailable path
// engages the rule-based fallback + template responder so the
// pipeline still meets the <=30s gate (no LLM dependency).
func runScenario8LLMUnavailable(t *testing.T, rec *e2eOutcomeRecorder) {
	tenantID := "tenant-e2e-8"
	llmErr := errors.New("bedrock 503 service unavailable")
	h := setupE2EHarness(t, e2eHarnessConfig{
		tenantID:      tenantID,
		channel:       "tiktok",
		classifierErr: llmErr,
		rephraseErr:   llmErr,
		faqEntries:    e2eFAQEntries(tenantID),
	})
	body := e2eTikTokRequestBody(t, tenantID, "m-s8", "How long does shipping to Sydney take?")
	resp, latency := runTikTokE2E(t, h, webhook.NewMemoryIdempotencyStore(), body)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (LLM-unavailable rule fallback)", resp.Code)
	}
	if latency > e2eDeadline {
		t.Fatalf("latency = %s, want <= %s", latency, e2eDeadline)
	}
	// Rule fallback for shipping_query has confidence 0.78 (>= 0.6
	// but <= 0.8) -> suggested. Template phrase source because the
	// LLM rephrase failed.
	if h.sender.calls.Load() != 0 {
		t.Fatalf("sender calls = %d, want 0 (rule-fallback shipping=0.78 routes suggested -- no auto-send)", h.sender.calls.Load())
	}
	if !busHasEscalated(h.bus) {
		t.Fatalf("missing escalation/suggested event for rule-fallback path")
	}
	evt := firstEscalatedEvent(h.bus)
	if got, _ := evt.Payload["outcome"].(string); got != "suggested" {
		t.Fatalf("rule-fallback outcome = %q, want suggested (0.78 confidence)", got)
	}
	rec.record(scenarioOutcome{name: "8_tiktok_llm_unavailable_rule", channel: "tiktok", httpCode: resp.Code, latency: latency, escalated: true})
}
