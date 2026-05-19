// File scope: v3.6.0 EC-8-1 enquiry classifier RED tests.
//
// Cite plan EC-8-1 acceptance:
//   - Bilingual EN/ZH with closed-enum intent taxonomy + sentiment.
//   - LLM-first via IronClaw, rule-based fallback, template fallback.
//   - Confidence < 0.6 -> flag for human review.
//
// Cite skill: test-driven-development; go-clean-architecture
// (port + adapter -- the classifier depends on port.AITextGenerator).
package customerservice

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

// fixedClassifierTime keeps deterministic timestamps in fixtures.
var fixedClassifierTime = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

// stubLLM returns a configured response/error pair on every Complete
// call. Used to drive the LLM-first / fallback / failover branches.
type stubLLM struct {
	response port.AICompletionResponse
	err      error
}

func (s *stubLLM) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return s.response, s.err
}

func newClassifierForTest(t *testing.T, llm port.AITextGenerator) *EnquiryClassifier {
	t.Helper()
	cls, err := NewEnquiryClassifier(nil, EnquiryClassifierConfig{
		Generator: llm,
		TenantID:  "tenant-cs",
		Now:       func() time.Time { return fixedClassifierTime },
	})
	if err != nil {
		t.Fatalf("NewEnquiryClassifier: %v", err)
	}
	t.Cleanup(func() { _ = cls.Close(context.Background()) })
	return cls
}

func TestEnquiryClassifier_DetectsRefundIntentEN(t *testing.T) {
	t.Parallel()
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: `{"intent":"refund_request","sentiment":"negative","language":"en","confidence":0.92}`,
	}}
	cls := newClassifierForTest(t, llm)
	res, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m1",
		Channel:   "tiktok",
		Text:      "I want a refund for my last order, this is unacceptable.",
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Intent != IntentRefundRequest {
		t.Fatalf("Intent = %q, want %q", res.Intent, IntentRefundRequest)
	}
	if res.Language != LanguageEN {
		t.Fatalf("Language = %q, want %q", res.Language, LanguageEN)
	}
	if res.Source != ClassifySourceLLM {
		t.Fatalf("Source = %q, want %q", res.Source, ClassifySourceLLM)
	}
	if res.Confidence < 0.6 {
		t.Fatalf("Confidence = %.2f, want >= 0.6", res.Confidence)
	}
}

func TestEnquiryClassifier_DetectsRefundIntentCN(t *testing.T) {
	t.Parallel()
	// Force LLM failure so the rule-based fallback runs and we prove
	// the keyword cascade catches the Chinese 退款 (tuikuan = refund).
	llm := &stubLLM{err: errors.New("bedrock: 503 service unavailable")}
	cls := newClassifierForTest(t, llm)
	res, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m2",
		Channel:   "tiktok",
		Text:      "你好，我想申请退款，这个产品我不满意。",
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Intent != IntentRefundRequest {
		t.Fatalf("Intent = %q, want %q (CN refund)", res.Intent, IntentRefundRequest)
	}
	if res.Language != LanguageZHCN {
		t.Fatalf("Language = %q, want %q (CN detection)", res.Language, LanguageZHCN)
	}
	if res.Source != ClassifySourceRule {
		t.Fatalf("Source = %q, want %q (rule fallback)", res.Source, ClassifySourceRule)
	}
}

func TestEnquiryClassifier_NegativeSentimentTriggersUrgent(t *testing.T) {
	t.Parallel()
	// LLM returns negative sentiment + complaint intent + the text
	// itself contains an urgency marker; the merge step must
	// upgrade sentiment to urgent (overrides per spec).
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: `{"intent":"complaint","sentiment":"negative","language":"en","confidence":0.85}`,
	}}
	cls := newClassifierForTest(t, llm)
	res, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m3",
		Channel:   "facebook",
		Text:      "URGENT! my package never arrived and you ignored my email!",
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Sentiment != SentimentUrgent {
		t.Fatalf("Sentiment = %q, want %q (urgency keyword overrides)", res.Sentiment, SentimentUrgent)
	}
}

func TestEnquiryClassifier_RuleFallbackOnLLMFailure(t *testing.T) {
	t.Parallel()
	// Table-driven across the four canonical failure shapes per
	// the v3.2.1 EC-2-1 failover precedent: nil-equivalent,
	// upstream 5xx, deadline exceeded, circuit breaker open.
	cases := []struct {
		name string
		err  error
	}{
		{"nil_equivalent", errors.New("ironclaw: not configured")},
		{"upstream_5xx", errors.New("bedrock: 503 service unavailable")},
		{"deadline_exceeded", context.DeadlineExceeded},
		{"breaker_open", errors.New("breaker: open")},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			llm := &stubLLM{err: tc.err}
			cls := newClassifierForTest(t, llm)
			res, err := cls.Classify(context.Background(), EnquiryRequest{
				MessageID: "m-" + tc.name,
				Channel:   "tiktok",
				Text:      "Where is my order? I need it shipped soon.",
			})
			if err != nil {
				t.Fatalf("Classify: %v", err)
			}
			if res.Source != ClassifySourceRule && res.Source != ClassifySourceTemplate {
				t.Fatalf("Source = %q, want rule or template (failover %s)", res.Source, tc.name)
			}
			// All four shipping-query messages must classify as
			// shipping_query / order_status. The keyword cascade
			// must not silently fall through to spam.
			if res.Intent != IntentShippingQuery && res.Intent != IntentOrderStatus {
				t.Fatalf("Intent = %q, want shipping_query|order_status (failover %s)", res.Intent, tc.name)
			}
		})
	}
}

func TestEnquiryClassifier_LowConfidenceFlagsForReview(t *testing.T) {
	t.Parallel()
	// LLM returns a confidence well below the 0.6 floor; the
	// classifier still returns the result but the FlagForReview
	// bit must be set so the EC-8-2 responder routes to escalation.
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: `{"intent":"general_enquiry","sentiment":"neutral","language":"en","confidence":0.32}`,
	}}
	cls := newClassifierForTest(t, llm)
	res, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m4",
		Channel:   "tiktok",
		Text:      "uh, hello? maybe? not sure.",
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if !res.FlagForReview {
		t.Fatalf("FlagForReview = false, want true (confidence %.2f < %.2f floor)", res.Confidence, DefaultLowConfidenceThreshold)
	}
}

// TestEnquiryClassifier_TaxonomyAcceptance is the 20-fixture
// table that exercises the full EN+CN x 8-intent taxonomy. Cite
// plan EC-8-1 acceptance ("Intent taxonomy (closed enum) ...") +
// "RED tests: ... TestEnquiryClassifier_TaxonomyAcceptance over 20
// fixtures (mix of EN/CN, all 8 intents)".
func TestEnquiryClassifier_TaxonomyAcceptance(t *testing.T) {
	t.Parallel()
	type fixture struct {
		name    string
		text    string
		want    Intent
		lang    Language
		llmJSON string
	}
	cases := []fixture{
		// 1-2: order_status EN+CN
		{"order_status_en", "Where is my order #12345?", IntentOrderStatus, LanguageEN, `{"intent":"order_status","sentiment":"neutral","language":"en","confidence":0.9}`},
		{"order_status_cn", "我的订单 #12345 在哪里？", IntentOrderStatus, LanguageZHCN, `{"intent":"order_status","sentiment":"neutral","language":"zh-cn","confidence":0.88}`},
		// 3-4: refund_request EN+CN
		{"refund_request_en", "Please process my refund for order 999.", IntentRefundRequest, LanguageEN, `{"intent":"refund_request","sentiment":"negative","language":"en","confidence":0.91}`},
		{"refund_request_cn", "请帮我处理订单的退款。", IntentRefundRequest, LanguageZHCN, `{"intent":"refund_request","sentiment":"negative","language":"zh-cn","confidence":0.87}`},
		// 5-6: product_question EN+CN
		{"product_question_en", "Does this phone case fit iPhone 15 Pro Max?", IntentProductQuestion, LanguageEN, `{"intent":"product_question","sentiment":"neutral","language":"en","confidence":0.83}`},
		{"product_question_cn", "请问这个手机壳适合iPhone 15 Pro Max吗？", IntentProductQuestion, LanguageZHCN, `{"intent":"product_question","sentiment":"neutral","language":"zh-cn","confidence":0.81}`},
		// 7-8: shipping_query EN+CN
		{"shipping_query_en", "How long does shipping take to Sydney?", IntentShippingQuery, LanguageEN, `{"intent":"shipping_query","sentiment":"neutral","language":"en","confidence":0.84}`},
		{"shipping_query_cn", "运输到悉尼需要多久？", IntentShippingQuery, LanguageZHCN, `{"intent":"shipping_query","sentiment":"neutral","language":"zh-cn","confidence":0.82}`},
		// 9-10: complaint EN+CN
		{"complaint_en", "This product broke after one day, terrible quality.", IntentComplaint, LanguageEN, `{"intent":"complaint","sentiment":"negative","language":"en","confidence":0.9}`},
		{"complaint_cn", "这个产品质量太差了，用了一天就坏了。", IntentComplaint, LanguageZHCN, `{"intent":"complaint","sentiment":"negative","language":"zh-cn","confidence":0.88}`},
		// 11-12: compliment EN+CN
		{"compliment_en", "Love this product! Best purchase of the year.", IntentCompliment, LanguageEN, `{"intent":"compliment","sentiment":"positive","language":"en","confidence":0.94}`},
		{"compliment_cn", "这个产品太棒了，今年最佳购物！", IntentCompliment, LanguageZHCN, `{"intent":"compliment","sentiment":"positive","language":"zh-cn","confidence":0.92}`},
		// 13-14: general_enquiry EN+CN
		{"general_enquiry_en", "Hello, can I ask a question about your store?", IntentGeneralEnquiry, LanguageEN, `{"intent":"general_enquiry","sentiment":"neutral","language":"en","confidence":0.8}`},
		{"general_enquiry_cn", "你好，能问个关于你们店的问题吗？", IntentGeneralEnquiry, LanguageZHCN, `{"intent":"general_enquiry","sentiment":"neutral","language":"zh-cn","confidence":0.78}`},
		// 15-16: spam EN+CN
		{"spam_en", "BUY CRYPTO NOW!!! 1000% RETURNS GUARANTEED!!!", IntentSpam, LanguageEN, `{"intent":"spam","sentiment":"neutral","language":"en","confidence":0.96}`},
		{"spam_cn", "立即购买加密货币！！！保证 1000% 回报！！！", IntentSpam, LanguageZHCN, `{"intent":"spam","sentiment":"neutral","language":"zh-cn","confidence":0.95}`},
		// 17-18: traditional Chinese routing
		{"refund_request_tw", "請幫我處理訂單的退款。", IntentRefundRequest, LanguageZHTW, `{"intent":"refund_request","sentiment":"negative","language":"zh-tw","confidence":0.87}`},
		{"shipping_query_tw", "運送到雪梨需要多久？", IntentShippingQuery, LanguageZHTW, `{"intent":"shipping_query","sentiment":"neutral","language":"zh-tw","confidence":0.83}`},
		// 19-20: other-language detection (Spanish) + emoji-heavy spam
		{"general_enquiry_other", "Hola, tengo una pregunta sobre el envio.", IntentGeneralEnquiry, LanguageOther, `{"intent":"general_enquiry","sentiment":"neutral","language":"other","confidence":0.7}`},
		{"spam_emoji", "🎰🎰🎰 WIN BIG NOW 🎰🎰🎰 click here", IntentSpam, LanguageEN, `{"intent":"spam","sentiment":"neutral","language":"en","confidence":0.97}`},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			llm := &stubLLM{response: port.AICompletionResponse{Content: tc.llmJSON}}
			cls := newClassifierForTest(t, llm)
			res, err := cls.Classify(context.Background(), EnquiryRequest{
				MessageID: "m-" + tc.name,
				Channel:   "tiktok",
				Text:      tc.text,
			})
			if err != nil {
				t.Fatalf("Classify(%s): %v", tc.name, err)
			}
			if res.Intent != tc.want {
				t.Fatalf("Intent(%s) = %q, want %q", tc.name, res.Intent, tc.want)
			}
			if res.Language != tc.lang {
				t.Fatalf("Language(%s) = %q, want %q", tc.name, res.Language, tc.lang)
			}
		})
	}
}

func TestEnquiryClassifier_TemplateFallbackWhenLLMAndRuleSilent(t *testing.T) {
	t.Parallel()
	// Both LLM (errored) and rule-based fallback (matches nothing
	// recognisable) come back without a strong signal -- the
	// classifier must still return a result with the template
	// outcome (general_enquiry) and FlagForReview = true so the
	// pipeline never blocks the EC-8-3 inbound webhook.
	llm := &stubLLM{err: errors.New("breaker: open")}
	cls := newClassifierForTest(t, llm)
	res, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m5",
		Channel:   "tiktok",
		Text:      "qwertyzxcv mlkj asdf nope!",
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if res.Source != ClassifySourceTemplate {
		t.Fatalf("Source = %q, want %q (template fallback)", res.Source, ClassifySourceTemplate)
	}
	if !res.FlagForReview {
		t.Fatalf("FlagForReview = false, want true (template fallback always escalates)")
	}
}

func TestEnquiryClassifier_RejectsClosedClassifier(t *testing.T) {
	t.Parallel()
	cls := newClassifierForTest(t, &stubLLM{})
	if err := cls.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_, err := cls.Classify(context.Background(), EnquiryRequest{MessageID: "m", Channel: "tiktok", Text: "hi"})
	if !errors.Is(err, ErrClassifierClosed) {
		t.Fatalf("err = %v, want ErrClassifierClosed", err)
	}
}

func TestEnquiryClassifier_RejectsUnsupportedLanguageOverride(t *testing.T) {
	t.Parallel()
	cls := newClassifierForTest(t, &stubLLM{})
	_, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID:        "m",
		Channel:          "tiktok",
		Text:             "hi",
		LanguageOverride: "klingon",
	})
	if !errors.Is(err, ErrUnsupportedLanguage) {
		t.Fatalf("err = %v, want ErrUnsupportedLanguage", err)
	}
}

func TestNewEnquiryClassifier_ConfigValidation(t *testing.T) {
	t.Parallel()
	cases := map[string]EnquiryClassifierConfig{
		"missing_generator": {TenantID: "t"},
		"missing_tenant":    {Generator: &stubLLM{}},
	}
	for name, cfg := range cases {
		name, cfg := name, cfg
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			_, err := NewEnquiryClassifier(nil, cfg)
			if !errors.Is(err, ErrClassifierUnconfigured) {
				t.Fatalf("err = %v, want ErrClassifierUnconfigured", err)
			}
		})
	}
}

// recordingMetrics captures classification + faq metric emissions
// in-process so the resilience pillar gate (every package emits
// Prometheus counters) is provable without spinning a registry.
type recordingMetrics struct {
	classifications []classificationCall
	faqResponses    []faqCall
}

type classificationCall struct {
	tenantID, intent, sentiment, language string
}

type faqCall struct {
	tenantID, outcome string
}

func (m *recordingMetrics) RecordClassification(tenantID, intent, sentiment, language string) {
	m.classifications = append(m.classifications, classificationCall{tenantID, intent, sentiment, language})
}

func (m *recordingMetrics) RecordFAQResponse(tenantID, outcome string) {
	m.faqResponses = append(m.faqResponses, faqCall{tenantID, outcome})
}

func TestEnquiryClassifier_EmitsMetrics(t *testing.T) {
	t.Parallel()
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: `{"intent":"refund_request","sentiment":"negative","language":"en","confidence":0.9}`,
	}}
	metrics := &recordingMetrics{}
	cls, err := NewEnquiryClassifier(nil, EnquiryClassifierConfig{
		Generator: llm,
		TenantID:  "tenant-cs",
		Metrics:   metrics,
		Now:       func() time.Time { return fixedClassifierTime },
	})
	if err != nil {
		t.Fatalf("NewEnquiryClassifier: %v", err)
	}
	t.Cleanup(func() { _ = cls.Close(context.Background()) })

	if _, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m6",
		Channel:   "tiktok",
		Text:      "I want a refund please",
	}); err != nil {
		t.Fatalf("Classify: %v", err)
	}
	if len(metrics.classifications) != 1 {
		t.Fatalf("classifications = %d, want 1", len(metrics.classifications))
	}
	got := metrics.classifications[0]
	if got.intent != string(IntentRefundRequest) || got.sentiment != string(SentimentNegative) {
		t.Fatalf("metric labels = %+v", got)
	}
}

func TestEnquiryClassifier_EmptyMessageRejected(t *testing.T) {
	t.Parallel()
	cls := newClassifierForTest(t, &stubLLM{})
	_, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m",
		Channel:   "tiktok",
		Text:      "    ",
	})
	if !errors.Is(err, ErrClassifierUnconfigured) {
		t.Fatalf("err = %v, want ErrClassifierUnconfigured (empty text)", err)
	}
}

// TestEnquiryClassifier_LLMResponseUnparseable proves the rule
// fallback runs when the LLM returns a non-JSON blob, so the
// pipeline never wedges on a malformed upstream.
func TestEnquiryClassifier_LLMResponseUnparseable(t *testing.T) {
	t.Parallel()
	llm := &stubLLM{response: port.AICompletionResponse{
		Content: "this is not JSON, sorry!",
	}}
	cls := newClassifierForTest(t, llm)
	res, err := cls.Classify(context.Background(), EnquiryRequest{
		MessageID: "m7",
		Channel:   "tiktok",
		Text:      "I want a refund",
	})
	if err != nil {
		t.Fatalf("Classify: %v", err)
	}
	// LLM didn't error -- but the response was unusable -- so the
	// rule cascade fires and the source is rule.
	if res.Source != ClassifySourceRule {
		t.Fatalf("Source = %q, want %q", res.Source, ClassifySourceRule)
	}
	if res.Intent != IntentRefundRequest {
		t.Fatalf("Intent = %q, want %q (rule cascade)", res.Intent, IntentRefundRequest)
	}
}

// TestIntent_AllSupported documents the closed enum so the
// taxonomy gate is testable as a pure function.
func TestIntent_AllSupported(t *testing.T) {
	t.Parallel()
	want := []Intent{
		IntentOrderStatus, IntentRefundRequest, IntentProductQuestion,
		IntentShippingQuery, IntentComplaint, IntentCompliment,
		IntentGeneralEnquiry, IntentSpam,
	}
	got := SupportedIntents()
	if len(got) != len(want) {
		t.Fatalf("supported = %d, want %d", len(got), len(want))
	}
	for i, intent := range got {
		if intent != want[i] {
			t.Fatalf("supported[%d] = %q, want %q", i, intent, want[i])
		}
	}
}

func TestLanguage_AllSupported(t *testing.T) {
	t.Parallel()
	want := []Language{LanguageEN, LanguageZHCN, LanguageZHTW, LanguageOther}
	got := SupportedLanguages()
	if len(got) != len(want) {
		t.Fatalf("supported = %d, want %d", len(got), len(want))
	}
	for i, lang := range got {
		if lang != want[i] {
			t.Fatalf("supported[%d] = %q, want %q", i, lang, want[i])
		}
	}
}

// TestSentiment_UrgentOverridesNegative ensures the merge rule is
// pure-function testable so reviewers can trust the override
// without re-reading the orchestration body.
func TestSentiment_UrgentOverridesNegative(t *testing.T) {
	t.Parallel()
	if got := mergeSentiment(SentimentNegative, "URGENT please reply"); got != SentimentUrgent {
		t.Fatalf("got = %q, want urgent", got)
	}
	if got := mergeSentiment(SentimentNegative, "this is fine"); got != SentimentNegative {
		t.Fatalf("got = %q, want negative", got)
	}
	if got := mergeSentiment(SentimentPositive, "love this"); got != SentimentPositive {
		t.Fatalf("got = %q, want positive", got)
	}
}

// TestDetectLanguage_TableDriven exercises the language detector in
// isolation so reviewers can trust the heuristic without re-reading
// the LLM/rule plumbing.
func TestDetectLanguage_TableDriven(t *testing.T) {
	t.Parallel()
	cases := map[string]Language{
		"plain english":       LanguageEN,
		"hello world":         LanguageEN,
		"你好世界":                LanguageZHCN,
		"請幫我處理":               LanguageZHTW,
		"Hola que tal":        LanguageOther,
		"":                    LanguageEN,
		"123 456":             LanguageEN,
		"mixed 你好 hi":         LanguageZHCN,
		"mostly english 一点中文": LanguageZHCN,
	}
	for input, want := range cases {
		input, want := input, want
		t.Run(strings.ReplaceAll(input, " ", "_"), func(t *testing.T) {
			t.Parallel()
			if got := detectLanguageHeuristic(input); got != want {
				t.Fatalf("detectLanguageHeuristic(%q) = %q, want %q", input, got, want)
			}
		})
	}
}
