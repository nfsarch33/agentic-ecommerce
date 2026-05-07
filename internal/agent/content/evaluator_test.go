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

func TestEvaluatorQualityDimensions(t *testing.T) {
	t.Parallel()

	evaluator := content.NewEvaluator()
	tests := []struct {
		name   string
		input  content.EvaluationInput
		assert func(*testing.T, content.Evaluation)
	}{
		{
			name: "missing keyword fails density without failing unrelated fields",
			input: content.EvaluationInput{
				Product: content.ProductInfo{Title: "Resistance Band Set"},
				Output: content.GeneratedContent{
					Description:     "Resistance Band Set makes short home workouts simple and practical.",
					SEOTitle:        "Resistance Band Set",
					MetaDescription: "Train at home with a compact resistance band set.",
				},
				Style:    content.StyleProfessional,
				MaxWords: 50,
				Keywords: []string{
					"resistance band set",
					"travel recovery",
				},
			},
			assert: func(t *testing.T, got content.Evaluation) {
				t.Helper()
				if got.KeywordDensity["travel recovery"] != 0 {
					t.Fatalf("missing keyword density = %.2f, want 0", got.KeywordDensity["travel recovery"])
				}
				if !got.Length.WithinLimit || !got.Tone.Pass || len(got.FactualIssues) != 0 {
					t.Fatalf("unrelated dimensions failed: %+v", got)
				}
			},
		},
		{
			name: "tone flags casual copy in professional style",
			input: content.EvaluationInput{
				Product: content.ProductInfo{Title: "Resistance Band Set"},
				Output: content.GeneratedContent{
					Description:     "Resistance Band Set is an awesome pick for super quick home workouts.",
					SEOTitle:        "Resistance Band Set",
					MetaDescription: "Awesome resistance band set for simple home training.",
				},
				Style:    content.StyleProfessional,
				MaxWords: 50,
			},
			assert: func(t *testing.T, got content.Evaluation) {
				t.Helper()
				if got.Tone.Pass || len(got.Tone.Issues) == 0 {
					t.Fatalf("tone = %+v, want professional tone issue", got.Tone)
				}
			},
		},
		{
			name: "readability penalty keeps score below clean copy",
			input: content.EvaluationInput{
				Product: content.ProductInfo{Title: "Resistance Band Set"},
				Output: content.GeneratedContent{
					Description:     "Resistance Band Set utilises multidisciplinary physiological optimisation methodologies for comprehensive musculoskeletal conditioning adaptation.",
					SEOTitle:        "Resistance Band Set",
					MetaDescription: "Resistance Band Set for advanced conditioning.",
				},
				Style:    content.StyleProfessional,
				MaxWords: 50,
			},
			assert: func(t *testing.T, got content.Evaluation) {
				t.Helper()
				if got.ReadabilityScore >= 50 {
					t.Fatalf("readability = %.2f, want difficult copy below 50", got.ReadabilityScore)
				}
				if got.Score > 88 {
					t.Fatalf("score = %d, want readability penalty applied", got.Score)
				}
			},
		},
		{
			name: "factual check flags missing product title",
			input: content.EvaluationInput{
				Product: content.ProductInfo{Title: "Resistance Band Set"},
				Output: content.GeneratedContent{
					Description:     "Compact training gear supports warm ups and mobility anywhere.",
					SEOTitle:        "Home Workout Kit",
					MetaDescription: "A portable training kit for strength and recovery.",
				},
				Style:    content.StyleProfessional,
				MaxWords: 50,
			},
			assert: func(t *testing.T, got content.Evaluation) {
				t.Helper()
				if len(got.FactualIssues) == 0 {
					t.Fatalf("factual issues empty, want missing title issue")
				}
				if got.Pass {
					t.Fatalf("Pass = true, want factual issue to fail evaluation")
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			tt.assert(t, evaluator.Evaluate(tt.input))
		})
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
