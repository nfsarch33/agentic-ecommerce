package compliance

import (
	"testing"

	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
)

// File scope: covers the compliance helper functions that previously
// sat between 57% and 75% (normalizeEvaluation, decodePayload, mustMap).

func TestNormalizeEvaluationFillsNilCollections(t *testing.T) {
	t.Parallel()

	in := contentagent.Evaluation{}
	got := normalizeEvaluation(in)
	if got.KeywordDensity == nil {
		t.Fatal("KeywordDensity should be non-nil after normalize")
	}
	if got.Tone.Issues == nil {
		t.Fatal("Tone.Issues should be non-nil after normalize")
	}
	if got.FactualIssues == nil {
		t.Fatal("FactualIssues should be non-nil after normalize")
	}
}

func TestNormalizeEvaluationPreservesPopulatedCollections(t *testing.T) {
	t.Parallel()

	in := contentagent.Evaluation{
		KeywordDensity: map[string]float64{"home": 0.5},
		Tone: contentagent.ToneEvaluation{
			Pass:   false,
			Issues: []string{"too aggressive"},
		},
		FactualIssues: []string{"missing dimension"},
	}
	got := normalizeEvaluation(in)
	if got.KeywordDensity["home"] != 0.5 {
		t.Fatalf("keyword density = %v", got.KeywordDensity)
	}
	if len(got.Tone.Issues) != 1 || got.Tone.Issues[0] != "too aggressive" {
		t.Fatalf("tone = %+v", got.Tone)
	}
	if len(got.FactualIssues) != 1 {
		t.Fatalf("factual = %v", got.FactualIssues)
	}
}

func TestDecodePayloadHandlesUnmarshalError(t *testing.T) {
	t.Parallel()

	var dst int
	if err := decodePayload(map[string]any{"foo": 1}, &dst); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestMustMapHandlesNonSerialisableInput(t *testing.T) {
	t.Parallel()

	if got := mustMap(make(chan struct{})); len(got) != 0 {
		t.Fatalf("mustMap(channel) = %v, want empty", got)
	}
}

func TestFailureReasonsCollectsAllSources(t *testing.T) {
	t.Parallel()

	eval := contentagent.Evaluation{
		Length: contentagent.LengthEvaluation{WithinLimit: false},
		Tone: contentagent.ToneEvaluation{
			Pass:   false,
			Issues: []string{"slightly off-brand"},
		},
		FactualIssues:  []string{"price mismatch"},
		KeywordDensity: map[string]float64{"home workout": 0, "resistance": 0.05},
	}
	reasons := failureReasons(eval)

	mustContain := func(want string) {
		t.Helper()
		for _, reason := range reasons {
			if reason == want {
				return
			}
		}
		t.Fatalf("reasons %v missing %q", reasons, want)
	}

	mustContain("content exceeds max words")
	mustContain("slightly off-brand")
	mustContain("price mismatch")
	mustContain("missing keyword: home workout")
}
