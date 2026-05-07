package content_test

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
)

func TestEvaluatorGoldenQualityScore(t *testing.T) {
	t.Parallel()

	evaluator := content.NewEvaluator()
	got := evaluator.Evaluate(content.EvaluationInput{
		Product: content.ProductInfo{
			ID:    "b1000000-0000-0000-0000-000000000001",
			SKU:   "RB-SET-5",
			Title: "Resistance Band Set",
		},
		Output: content.GeneratedContent{
			Description:     "Resistance Band Set makes home workouts simple. This resistance band set includes five tension levels for warm ups, strength work, and travel training.",
			SEOTitle:        "Resistance Band Set for Home Workouts",
			MetaDescription: "Shop a compact resistance band set for home workouts, warm ups, and travel strength training.",
		},
		Style:    content.StyleProfessional,
		MaxWords: 55,
		Keywords: []string{
			"resistance band set",
			"home workouts",
		},
	})

	assertGoldenJSONPayload(t, filepath.Join("testdata", "evaluation.golden.json"), got)
}

func TestEvaluatorFlagsPlaceholdersAndLength(t *testing.T) {
	t.Parallel()

	evaluator := content.NewEvaluator()
	got := evaluator.Evaluate(content.EvaluationInput{
		Product: content.ProductInfo{SKU: "RB-SET-5", Title: "Resistance Band Set"},
		Output: content.GeneratedContent{
			Description:     "TODO write an amazing product description with [material] and [benefit] repeated many many many many many many times.",
			SEOTitle:        "TODO title",
			MetaDescription: "{{meta description}}",
		},
		MaxWords: 8,
	})

	if got.Pass {
		t.Fatalf("Pass = true, want false: %+v", got)
	}
	if got.Length.WithinLimit {
		t.Fatalf("Length.WithinLimit = true, want false: %+v", got.Length)
	}
	if len(got.FactualIssues) == 0 {
		t.Fatalf("FactualIssues empty, want placeholder issue")
	}
}

func assertGoldenJSONPayload(t *testing.T, goldenPath string, actualPayload any) {
	t.Helper()
	actual, err := json.MarshalIndent(actualPayload, "", "  ")
	if err != nil {
		t.Fatalf("marshal actual payload: %v", err)
	}
	actual = append(actual, '\n')
	want, err := os.ReadFile(goldenPath)
	if err != nil {
		t.Fatalf("read golden JSON: %v", err)
	}
	var wantPayload, actualDecoded any
	if err := json.Unmarshal(want, &wantPayload); err != nil {
		t.Fatalf("decode golden JSON: %v", err)
	}
	if err := json.Unmarshal(actual, &actualDecoded); err != nil {
		t.Fatalf("decode actual JSON: %v", err)
	}
	if !reflect.DeepEqual(wantPayload, actualDecoded) {
		t.Fatalf("golden JSON mismatch\nwant:\n%s\ngot:\n%s", want, bytes.TrimSpace(actual))
	}
}
