// File scope: v3.6.0 EC-8-2 FAQ responder RED tests.
//
// Cite plan EC-8-2 acceptance:
//   - Auto-reply gate: confidence >0.8 -> auto-send; 0.6-0.8 ->
//     suggest to operator; <0.6 -> escalate.
//   - Typed errors: ErrNoFAQMatch, ErrFAQResponseTooLong,
//     ErrFAQLLMUnavailable.
//   - Reuse the v3.2.0 LLM failover pattern (port.AITextGenerator).
package customerservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// inMemoryFAQStore is the test double for port.FAQStore. The
// production adapter (Postgres-backed; v3.6.1 wires it) implements
// the same Search contract; see migrations/0015_faq_entries.up.sql.
type inMemoryFAQStore struct {
	entries []FAQEntry
}

func (s *inMemoryFAQStore) Search(_ context.Context, query FAQSearchQuery) ([]FAQEntry, error) {
	out := make([]FAQEntry, 0, len(s.entries))
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

func newFAQResponderForTest(t *testing.T, llm port.AITextGenerator, store FAQStore) *FAQResponder {
	t.Helper()
	r, err := NewFAQResponder(nil, FAQResponderConfig{
		Generator: llm,
		Store:     store,
		TenantID:  "tenant-cs",
		Now:       func() time.Time { return fixedClassifierTime },
	})
	if err != nil {
		t.Fatalf("NewFAQResponder: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })
	return r
}

// fixtureFAQEntries returns a small representative FAQ corpus the
// EC-8-2 responder ranks against. Mix of EN/CN entries across all
// the EC-8-1 supported intents.
func fixtureFAQEntries() []FAQEntry {
	return []FAQEntry{
		{
			TenantID: "tenant-cs", EntryID: "faq-1",
			Language: LanguageEN, IntentCategory: IntentShippingQuery,
			Question: "How long does shipping take to Australia?",
			Answer:   "Standard shipping to Australia takes 5-10 business days. Express options ship in 2-4 days.",
			Keywords: []string{"shipping", "australia", "delivery", "how long"},
		},
		{
			TenantID: "tenant-cs", EntryID: "faq-2",
			Language: LanguageEN, IntentCategory: IntentShippingQuery,
			Question: "How long does delivery to Sydney take?",
			Answer:   "Sydney metro deliveries typically arrive in 3-5 business days for standard shipping.",
			Keywords: []string{"shipping", "sydney", "delivery"},
		},
		{
			TenantID: "tenant-cs", EntryID: "faq-3",
			Language: LanguageEN, IntentCategory: IntentProductQuestion,
			Question: "Does the phone case fit iPhone 15 Pro?",
			Answer:   "Yes, our phone case is engineered for iPhone 15 Pro and iPhone 15 Pro Max.",
			Keywords: []string{"phone case", "iphone", "fit", "compatible"},
		},
		{
			TenantID: "tenant-cs", EntryID: "faq-4",
			Language: LanguageZHCN, IntentCategory: IntentShippingQuery,
			Question: "运送到悉尼需要多久？",
			Answer:   "悉尼市区配送通常 3-5 个工作日（标准运输）。",
			Keywords: []string{"悉尼", "运输", "几天"},
		},
		{
			TenantID: "tenant-cs", EntryID: "faq-5",
			Language: LanguageEN, IntentCategory: IntentGeneralEnquiry,
			Question: "What are your store hours?",
			Answer:   "We're an online store, open 24/7. Customer service replies between 9am and 5pm Sydney time.",
			Keywords: []string{"hours", "store", "support"},
		},
		// A different tenant's entry to prove tenant isolation.
		{
			TenantID: "tenant-other", EntryID: "faq-leak",
			Language: LanguageEN, IntentCategory: IntentShippingQuery,
			Question: "Other-tenant secret answer",
			Answer:   "This MUST NOT leak across tenants",
			Keywords: []string{"shipping"},
		},
	}
}

func TestFAQResponder_FindsHighConfidenceMatchAutoReplies(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: "Sydney metro deliveries typically arrive in 3-5 business days for standard shipping.",
	}}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m1",
		Channel:   "tiktok",
		Classification: EnquiryResult{
			MessageID: "m1", Intent: IntentShippingQuery,
			Sentiment: SentimentNeutral, Language: LanguageEN,
			Confidence: 0.92, Source: ClassifySourceLLM,
		},
		Text: "How long does shipping to Sydney take?",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if res.Outcome != FAQOutcomeAutoReplied {
		t.Fatalf("Outcome = %q, want %q", res.Outcome, FAQOutcomeAutoReplied)
	}
	if res.MatchedEntryID != "faq-2" {
		t.Fatalf("MatchedEntryID = %q, want faq-2 (Sydney match)", res.MatchedEntryID)
	}
	if res.Confidence < 0.8 {
		t.Fatalf("Confidence = %.2f, want >= 0.8 (auto-reply gate)", res.Confidence)
	}
	if !strings.Contains(strings.ToLower(res.ReplyText), "sydney") {
		t.Fatalf("ReplyText missing 'sydney': %q", res.ReplyText)
	}
}

func TestFAQResponder_SuggestsForMediumConfidence(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: "Standard shipping to Australia takes 5-10 business days. Express options ship in 2-4 days.",
	}}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m2",
		Channel:   "facebook",
		Classification: EnquiryResult{
			MessageID:  "m2",
			Intent:     IntentShippingQuery,
			Sentiment:  SentimentNeutral,
			Language:   LanguageEN,
			Confidence: 0.65,
			Source:     ClassifySourceLLM,
		},
		Text: "Maybe I want to know about delivery somewhere",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if res.Outcome != FAQOutcomeSuggested {
		t.Fatalf("Outcome = %q, want %q (medium confidence)", res.Outcome, FAQOutcomeSuggested)
	}
	if res.MatchedEntryID == "" {
		t.Fatalf("MatchedEntryID empty -- expected at least one shipping match")
	}
}

func TestFAQResponder_EscalatesForLowConfidence(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{response: port.AICompletionResponse{Content: "anything"}}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m3",
		Channel:   "tiktok",
		Classification: EnquiryResult{
			MessageID:     "m3",
			Intent:        IntentGeneralEnquiry,
			Sentiment:     SentimentNeutral,
			Language:      LanguageEN,
			Confidence:    0.4,
			Source:        ClassifySourceTemplate,
			FlagForReview: true,
		},
		Text: "??? what",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if res.Outcome != FAQOutcomeEscalated {
		t.Fatalf("Outcome = %q, want %q (low confidence escalates)", res.Outcome, FAQOutcomeEscalated)
	}
}

func TestFAQResponder_FallbacksToTemplateOnLLMFailure(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{err: errors.New("bedrock: 503 service unavailable")}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m4",
		Channel:   "tiktok",
		Classification: EnquiryResult{
			MessageID: "m4", Intent: IntentShippingQuery,
			Sentiment: SentimentNeutral, Language: LanguageEN,
			Confidence: 0.9, Source: ClassifySourceLLM,
		},
		Text: "shipping to Sydney?",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if res.PhraseSource != FAQPhraseSourceTemplate {
		t.Fatalf("PhraseSource = %q, want %q (LLM failed -> template)", res.PhraseSource, FAQPhraseSourceTemplate)
	}
	if res.MatchedEntryID == "" {
		t.Fatalf("MatchedEntryID empty -- the template path still surfaces a top match")
	}
}

func TestFAQResponder_NoMatchReturnsEscalation(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{response: port.AICompletionResponse{Content: "anything"}}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m5",
		Channel:   "tiktok",
		Classification: EnquiryResult{
			MessageID: "m5", Intent: IntentRefundRequest,
			Sentiment: SentimentNegative, Language: LanguageEN,
			Confidence: 0.95, Source: ClassifySourceLLM,
		},
		Text: "I want a refund",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if res.Outcome != FAQOutcomeEscalated {
		t.Fatalf("Outcome = %q, want %q (no faq match -> escalate)", res.Outcome, FAQOutcomeEscalated)
	}
	// The store had no refund_request entries -- the responder
	// surfaces the no-match condition via the typed sentinel on
	// the result, not a returned error (reply pipeline must always
	// keep flowing).
	if !errors.Is(res.MatchError, ErrNoFAQMatch) {
		t.Fatalf("MatchError = %v, want ErrNoFAQMatch", res.MatchError)
	}
}

func TestFAQResponder_TenantIsolation(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{response: port.AICompletionResponse{Content: "Sydney 3-5 days"}}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m6", Channel: "tiktok",
		Classification: EnquiryResult{
			MessageID: "m6", Intent: IntentShippingQuery,
			Sentiment: SentimentNeutral, Language: LanguageEN,
			Confidence: 0.9, Source: ClassifySourceLLM,
		},
		Text: "shipping to Sydney?",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	// faq-leak belongs to tenant-other and MUST NOT surface.
	if res.MatchedEntryID == "faq-leak" {
		t.Fatalf("tenant isolation breached: faq-leak surfaced")
	}
}

func TestFAQResponder_ResponseTooLongRejected(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	long := strings.Repeat("x", MaxFAQResponseLength+10)
	llm := &stubLLM{response: port.AICompletionResponse{Content: long}}
	r := newFAQResponderForTest(t, llm, store)
	res, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m7", Channel: "tiktok",
		Classification: EnquiryResult{
			MessageID: "m7", Intent: IntentShippingQuery,
			Sentiment: SentimentNeutral, Language: LanguageEN,
			Confidence: 0.9, Source: ClassifySourceLLM,
		},
		Text: "shipping?",
	})
	if err != nil {
		t.Fatalf("Respond: %v", err)
	}
	// The over-budget LLM reply forces template fallback so the
	// responder still surfaces a usable answer.
	if res.PhraseSource != FAQPhraseSourceTemplate {
		t.Fatalf("PhraseSource = %q, want %q", res.PhraseSource, FAQPhraseSourceTemplate)
	}
}

func TestFAQResponder_RejectsClosed(t *testing.T) {
	t.Parallel()
	r := newFAQResponderForTest(t, &stubLLM{}, &inMemoryFAQStore{})
	if err := r.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m", Channel: "tiktok",
		Classification: EnquiryResult{MessageID: "m", Intent: IntentShippingQuery, Confidence: 0.9},
		Text:           "hello",
	})
	if !errors.Is(err, ErrFAQResponderClosed) {
		t.Fatalf("err = %v, want ErrFAQResponderClosed", err)
	}
}

func TestFAQResponder_EmitsMetrics(t *testing.T) {
	t.Parallel()
	store := &inMemoryFAQStore{entries: fixtureFAQEntries()}
	llm := &stubLLM{response: port.AICompletionResponse{Content: "Sydney 3-5 days"}}
	metrics := &recordingMetrics{}
	r, err := NewFAQResponder(nil, FAQResponderConfig{
		Generator: llm, Store: store, TenantID: "tenant-cs",
		Metrics: metrics,
		Now:     func() time.Time { return fixedClassifierTime },
	})
	if err != nil {
		t.Fatalf("NewFAQResponder: %v", err)
	}
	t.Cleanup(func() { _ = r.Close(context.Background()) })

	if _, err := r.Respond(context.Background(), FAQRequest{
		MessageID: "m8", Channel: "tiktok",
		Classification: EnquiryResult{
			MessageID: "m8", Intent: IntentShippingQuery,
			Sentiment: SentimentNeutral, Language: LanguageEN,
			Confidence: 0.9, Source: ClassifySourceLLM,
		},
		Text: "Sydney shipping",
	}); err != nil {
		t.Fatalf("Respond: %v", err)
	}
	if len(metrics.faqResponses) != 1 {
		t.Fatalf("faqResponses = %d, want 1", len(metrics.faqResponses))
	}
}

func TestNewFAQResponder_ConfigValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]FAQResponderConfig{
		"missing_generator": {Store: &inMemoryFAQStore{}, TenantID: "t"},
		"missing_store":     {Generator: &stubLLM{}, TenantID: "t"},
		"missing_tenant":    {Generator: &stubLLM{}, Store: &inMemoryFAQStore{}},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewFAQResponder(nil, cfg)
			if !errors.Is(err, ErrFAQResponderUnconfigured) {
				t.Fatalf("err = %v, want ErrFAQResponderUnconfigured", err)
			}
		})
	}
}

// TestFAQRanker_BM25ScoresKeywordMatches isolates the BM25-style
// scorer so reviewers can trust the ranker without re-reading the
// orchestration.
func TestFAQRanker_BM25ScoresKeywordMatches(t *testing.T) {
	t.Parallel()
	entry := FAQEntry{
		EntryID: "faq", Question: "How long does shipping to Sydney take?",
		Keywords: []string{"shipping", "sydney"},
	}
	stronger := scoreFAQEntry(entry, "How long does shipping to Sydney take?")
	weaker := scoreFAQEntry(entry, "Hello world")
	if stronger <= weaker {
		t.Fatalf("expected stronger > weaker, got %.4f vs %.4f", stronger, weaker)
	}
}

// TestRouteByConfidence covers the three-tier gate as a pure
// function so reviewers do not need to chase the orchestration.
func TestRouteByConfidence(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		score   float64
		reviewF bool
		want    FAQOutcome
	}{
		{"auto", 0.9, false, FAQOutcomeAutoReplied},
		{"suggest_low_band", 0.65, false, FAQOutcomeSuggested},
		{"suggest_top", 0.79, false, FAQOutcomeSuggested},
		{"escalate_low", 0.5, false, FAQOutcomeEscalated},
		{"escalate_flag_overrides", 0.95, true, FAQOutcomeEscalated},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := routeByConfidence(tc.score, tc.reviewF)
			if got != tc.want {
				t.Fatalf("got = %q, want %q", got, tc.want)
			}
		})
	}
}
