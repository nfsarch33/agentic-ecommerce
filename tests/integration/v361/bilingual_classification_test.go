//go:build v361_smoke

// File scope: v3.6.1 QA Task 1 -- 50-fixture bilingual
// classification acceptance test (EC-8-1 hardening).
//
// Acceptance gates (cite plan + EC-8-1 hardening):
//   - >=85% overall accuracy across 50 curated fixtures.
//   - >=70% per-intent recall across the 8 intents.
//   - >=95% language-detection accuracy.
//   - All urgent+negative fixtures flagged with sentiment=urgent.
//   - All low-confidence (<0.6) fixtures FlagForReview=true.
//
// LLM is stubbed via a deterministic test fixture; rule fallback
// engages for keyword matches and template fallback engages for
// fixtures that match no rule. Per-intent confusion matrix is
// emitted to stdout via t.Log for debugging.
//
// Decomposition discipline (HARD GATE: complex_fn must NOT
// increase from 4 -- 11-sprint streak target):
//   - top-level tests stay thin orchestrators that delegate to
//     factory + assertion helpers
//   - fixture loading, classifier setup, and confusion-matrix
//     summarisation each live in focused functions below.
package v361

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/agent/customerservice"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

//go:embed fixtures/bilingual_50.json
var bilingual50FixturesRaw []byte

// bilingualFixture is one row from fixtures/bilingual_50.json.
// Pure value type so helpers can pass it around without aliasing.
type bilingualFixture struct {
	ID                    string  `json:"id"`
	Text                  string  `json:"text"`
	ExpectedIntent        string  `json:"expected_intent"`
	ExpectedSentiment     string  `json:"expected_sentiment"`
	ExpectedLanguage      string  `json:"expected_language"`
	ExpectedMinConfidence float64 `json:"expected_min_confidence"`
}

// bilingualClassificationResult captures the actual classifier
// output paired with the fixture it ran against. Used for the
// confusion matrix + per-intent / per-language rollups.
type bilingualClassificationResult struct {
	fixture bilingualFixture
	result  customerservice.EnquiryResult
	err     error
}

// loadBilingualFixtures decodes the embedded JSON corpus.
func loadBilingualFixtures(t *testing.T) []bilingualFixture {
	t.Helper()
	var fixtures []bilingualFixture
	if err := json.Unmarshal(bilingual50FixturesRaw, &fixtures); err != nil {
		t.Fatalf("decode bilingual_50.json: %v", err)
	}
	if len(fixtures) != 50 {
		t.Fatalf("fixture count = %d, want 50 (Epic 8 EC-8-1 hardening corpus)", len(fixtures))
	}
	return fixtures
}

// errLLMStub is the deterministic stub LLM error. Returned by
// stubLLMRuleFallback so every fixture exercises the rule cascade
// or the template fallback (never the live LLM path).
var errLLMStub = errors.New("v361: llm stubbed; rule fallback engaged")

// stubLLMRuleFallback implements port.AITextGenerator and always
// returns errLLMStub so the rule-fallback path is the one under
// test. Mirrors the v3.4.0 EC-5-1 video_script LLM-failover
// pattern.
type stubLLMRuleFallback struct{}

func (s stubLLMRuleFallback) Complete(_ context.Context, _ port.AICompletionRequest) (port.AICompletionResponse, error) {
	return port.AICompletionResponse{}, errLLMStub
}

// fixedClassifierTime keeps deterministic timestamps in fixtures.
var fixedClassifierTime = time.Date(2026, 5, 10, 12, 0, 0, 0, time.UTC)

// newBilingualClassifier builds the EC-8-1 classifier with the
// stub LLM wired in. Default low-confidence floor (0.6) -- the
// plan acceptance.
func newBilingualClassifier(t *testing.T) *customerservice.EnquiryClassifier {
	t.Helper()
	cls, err := customerservice.NewEnquiryClassifier(nil, customerservice.EnquiryClassifierConfig{
		Generator: stubLLMRuleFallback{},
		TenantID:  "tenant-v361",
		Now:       func() time.Time { return fixedClassifierTime },
	})
	if err != nil {
		t.Fatalf("NewEnquiryClassifier: %v", err)
	}
	t.Cleanup(func() { _ = cls.Close(context.Background()) })
	return cls
}

// runBilingualClassifier classifies every fixture and returns the
// paired (fixture, result, err) rows in fixture order. Centralises
// the loop so all 5 RED tests can reuse the same matrix.
func runBilingualClassifier(t *testing.T) []bilingualClassificationResult {
	t.Helper()
	fixtures := loadBilingualFixtures(t)
	cls := newBilingualClassifier(t)
	rows := make([]bilingualClassificationResult, 0, len(fixtures))
	for _, fx := range fixtures {
		res, err := cls.Classify(context.Background(), customerservice.EnquiryRequest{
			MessageID: fx.ID,
			TenantID:  "tenant-v361",
			Channel:   "tiktok",
			Text:      fx.Text,
		})
		rows = append(rows, bilingualClassificationResult{fixture: fx, result: res, err: err})
	}
	return rows
}

// confusionMatrix is keyed by [expectedIntent][actualIntent].
type confusionMatrix map[string]map[string]int

// buildConfusionMatrix tabulates the per-intent expected-vs-actual
// counts. Pure function so the formatting + assertions can split.
func buildConfusionMatrix(rows []bilingualClassificationResult) confusionMatrix {
	matrix := confusionMatrix{}
	for _, r := range rows {
		if r.err != nil {
			continue
		}
		exp := r.fixture.ExpectedIntent
		act := string(r.result.Intent)
		if matrix[exp] == nil {
			matrix[exp] = map[string]int{}
		}
		matrix[exp][act]++
	}
	return matrix
}

// formatConfusionMatrix produces a deterministic, human-readable
// confusion matrix for t.Log. Sorted intent rows; each row shows
// expected_intent then a histogram of actual_intent counts.
func formatConfusionMatrix(matrix confusionMatrix) string {
	expected := make([]string, 0, len(matrix))
	for k := range matrix {
		expected = append(expected, k)
	}
	sort.Strings(expected)
	var sb strings.Builder
	sb.WriteString("v3.6.1 bilingual 50-fixture confusion matrix\n")
	sb.WriteString("expected_intent -> {actual_intent: count}\n")
	for _, exp := range expected {
		sb.WriteString("  ")
		sb.WriteString(exp)
		sb.WriteString(": ")
		actuals := make([]string, 0, len(matrix[exp]))
		for k := range matrix[exp] {
			actuals = append(actuals, k)
		}
		sort.Strings(actuals)
		first := true
		for _, act := range actuals {
			if !first {
				sb.WriteString(", ")
			}
			first = false
			fmt.Fprintf(&sb, "%s=%d", act, matrix[exp][act])
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// TestBilingualClassification_50FixtureAcceptance is the overall
// accuracy gate. Acceptance: >=85% (Epic 8 EC-8-1 hardening).
func TestBilingualClassification_50FixtureAcceptance(t *testing.T) {
	t.Parallel()
	rows := runBilingualClassifier(t)
	correct := 0
	for _, r := range rows {
		if r.err != nil {
			continue
		}
		if string(r.result.Intent) == r.fixture.ExpectedIntent {
			correct++
		}
	}
	accuracy := float64(correct) / float64(len(rows))
	t.Logf("v3.6.1 bilingual 50-fixture accuracy: %d/%d = %.2f%%", correct, len(rows), accuracy*100)
	t.Log(formatConfusionMatrix(buildConfusionMatrix(rows)))
	if accuracy < 0.85 {
		t.Fatalf("accuracy = %.2f%%, want >= 85.00%% (Epic 8 EC-8-1 hardening)", accuracy*100)
	}
}

// perIntentRecall returns expected-count + correct-count keyed by
// expected intent.
func perIntentRecall(rows []bilingualClassificationResult) map[string][2]int {
	out := map[string][2]int{}
	for _, r := range rows {
		if r.err != nil {
			continue
		}
		exp := r.fixture.ExpectedIntent
		entry := out[exp]
		entry[0]++
		if string(r.result.Intent) == exp {
			entry[1]++
		}
		out[exp] = entry
	}
	return out
}

// TestBilingualClassification_PerIntentRecall verifies every
// intent meets the >=70% per-intent recall floor. The taxonomy
// is fixed; missing recall on any single intent fails the gate.
func TestBilingualClassification_PerIntentRecall(t *testing.T) {
	t.Parallel()
	rows := runBilingualClassifier(t)
	recall := perIntentRecall(rows)
	intents := make([]string, 0, len(recall))
	for k := range recall {
		intents = append(intents, k)
	}
	sort.Strings(intents)
	for _, intent := range intents {
		entry := recall[intent]
		if entry[0] == 0 {
			continue
		}
		ratio := float64(entry[1]) / float64(entry[0])
		t.Logf("v3.6.1 per-intent recall: %-18s %d/%d = %.2f%%", intent, entry[1], entry[0], ratio*100)
		if ratio < 0.70 {
			t.Fatalf("per-intent recall for %s = %.2f%%, want >= 70.00%%", intent, ratio*100)
		}
	}
}

// TestBilingualClassification_LanguageDetectionAccuracy verifies
// the >=95% language-detection floor (Epic 8 EC-8-1 hardening).
func TestBilingualClassification_LanguageDetectionAccuracy(t *testing.T) {
	t.Parallel()
	rows := runBilingualClassifier(t)
	correct := 0
	misses := []string{}
	for _, r := range rows {
		if r.err != nil {
			continue
		}
		if string(r.result.Language) == r.fixture.ExpectedLanguage {
			correct++
			continue
		}
		misses = append(misses, fmt.Sprintf("%s(want=%s,got=%s)", r.fixture.ID, r.fixture.ExpectedLanguage, r.result.Language))
	}
	accuracy := float64(correct) / float64(len(rows))
	t.Logf("v3.6.1 language detection accuracy: %d/%d = %.2f%% misses=%v", correct, len(rows), accuracy*100, misses)
	if accuracy < 0.95 {
		t.Fatalf("language detection accuracy = %.2f%%, want >= 95.00%%", accuracy*100)
	}
}

// TestBilingualClassification_NegativeSentimentEscalates verifies
// every fixture marked expected_sentiment=urgent (i.e. negative
// content + urgency keyword) ends up with sentiment=urgent.
//
// The mergeSentiment override fires when the rule cascade returns
// a non-positive sentiment AND the text contains an urgency
// keyword (urgent, asap, immediately, 紧急, 急, 立刻, ...).
// Acceptance criterion: ALL such fixtures flagged.
func TestBilingualClassification_NegativeSentimentEscalates(t *testing.T) {
	t.Parallel()
	rows := runBilingualClassifier(t)
	urgentCount := 0
	for _, r := range rows {
		if r.fixture.ExpectedSentiment != string(customerservice.SentimentUrgent) {
			continue
		}
		urgentCount++
		if r.err != nil {
			t.Errorf("urgent fixture %s errored: %v", r.fixture.ID, r.err)
			continue
		}
		if r.result.Sentiment != customerservice.SentimentUrgent {
			t.Errorf("fixture %s sentiment = %s, want urgent (negative + urgency keyword must escalate)", r.fixture.ID, r.result.Sentiment)
		}
	}
	if urgentCount == 0 {
		t.Fatalf("no urgent-sentiment fixtures found in corpus -- gate ineffective")
	}
	t.Logf("v3.6.1 urgent escalation: %d urgent-sentiment fixtures all flagged urgent", urgentCount)
}

// TestBilingualClassification_LowConfidenceFlagsForHumanReview
// verifies every fixture with expected confidence < 0.6 has
// FlagForReview=true (routed to operator). The default
// LowConfidenceFloor is 0.6 (Epic 8 EC-8-1 hardening).
func TestBilingualClassification_LowConfidenceFlagsForHumanReview(t *testing.T) {
	t.Parallel()
	rows := runBilingualClassifier(t)
	lowCount := 0
	for _, r := range rows {
		if r.fixture.ExpectedMinConfidence >= 0.6 {
			continue
		}
		lowCount++
		if r.err != nil {
			t.Errorf("low-confidence fixture %s errored: %v", r.fixture.ID, r.err)
			continue
		}
		if !r.result.FlagForReview {
			t.Errorf("fixture %s FlagForReview=false; want true (confidence=%.2f < 0.6 floor)", r.fixture.ID, r.result.Confidence)
		}
		if r.result.Confidence >= 0.6 {
			t.Errorf("fixture %s confidence = %.2f, want < 0.6 (template fallback floor)", r.fixture.ID, r.result.Confidence)
		}
	}
	if lowCount == 0 {
		t.Fatalf("no low-confidence fixtures found in corpus -- gate ineffective")
	}
	t.Logf("v3.6.1 low-confidence escalation: %d low-confidence fixtures all routed to operator", lowCount)
}
