package compliance

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/domain/catalog"
)

func TestEnginePassesCompliantProductContent(t *testing.T) {
	t.Parallel()

	product := testComplianceProduct(t, catalog.ProductInput{
		SKU:         "RB-SET-5",
		Title:       "Premium Resistance Band Set",
		Description: "Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training. Includes handles, anchors, and a carry bag for daily training.",
		Images: []catalog.Image{{
			URL: "https://cdn.example.com/rb.jpg",
			Alt: "Premium resistance band set with handles",
		}},
	})
	engine := NewEngine(DefaultRules())

	got := engine.Evaluate(context.Background(), ProductContent{
		Product:     product,
		Keywords:    []string{"resistance band set", "home workouts"},
		SEOTitle:    "Premium Resistance Band Set for Home Workouts",
		Meta:        "Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training.",
		SEOScoreMin: 70,
	})

	if !got.Pass {
		t.Fatalf("result = %#v, want pass", got)
	}
	if got.Score < 90 {
		t.Fatalf("score = %d, want >= 90", got.Score)
	}
	if len(got.Reasons) != 0 {
		t.Fatalf("reasons = %#v, want none", got.Reasons)
	}
}

func TestEngineReportsRuleIDsSeverityAndReasons(t *testing.T) {
	t.Parallel()

	product := testComplianceProduct(t, catalog.ProductInput{
		SKU:         "BAD-1",
		Title:       " ",
		Description: "Cheap miracle cure.",
		Images:      []catalog.Image{{URL: "https://cdn.example.com/bad.jpg"}},
	})
	engine := NewEngine(DefaultRules())

	got := engine.Evaluate(context.Background(), ProductContent{
		Product:     product,
		Keywords:    []string{"miracle cure"},
		SEOTitle:    "",
		Meta:        "",
		SEOScoreMin: 85,
	})

	if got.Pass {
		t.Fatal("expected compliance failure")
	}
	for _, want := range []string{"required_title", "description_length", "prohibited_words", "image_alt_text", "seo_minimum_score", "legal_disclaimer"} {
		if !hasRuleID(got.RuleIDs, want) {
			t.Fatalf("rule IDs = %#v, missing %q", got.RuleIDs, want)
		}
	}
	if got.Severity != SeverityCritical {
		t.Fatalf("severity = %q, want critical", got.Severity)
	}
	if len(got.Reasons) < 6 {
		t.Fatalf("reasons = %#v, want rule failure reasons", got.Reasons)
	}
}

func TestEngineComplianceMatrixGolden(t *testing.T) {
	t.Parallel()

	engine := NewEngine(DefaultRules())
	cases := []struct {
		name    string
		content ProductContent
	}{
		{
			name: "compliant_publish_candidate",
			content: ProductContent{
				Product: testComplianceProduct(t, catalog.ProductInput{
					SKU:         "RB-SET-5",
					Title:       "Premium Resistance Band Set for Home Workouts",
					Description: "Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training. Includes handles, anchors, and a carry bag for daily training.",
					Images:      []catalog.Image{{URL: "https://cdn.example.com/rb.jpg", Alt: "Premium resistance band set with handles"}},
				}),
				Keywords:    []string{"resistance band set", "home workouts"},
				SEOTitle:    "Premium Resistance Band Set for Home Workouts",
				Meta:        "Premium resistance band set for home workouts, warm ups, rehab, and progressive strength training.",
				SEOScoreMin: 70,
			},
		},
		{
			name: "critical_claims_and_missing_media",
			content: ProductContent{
				Product: testComplianceProduct(t, catalog.ProductInput{
					SKU:         "BAD-1",
					Title:       " ",
					Description: "Cheap miracle cure.",
					Images:      []catalog.Image{{URL: "https://cdn.example.com/bad.jpg"}},
				}),
				Keywords:    []string{"miracle cure"},
				SEOScoreMin: 85,
			},
		},
		{
			name: "seo_threshold_failure_only",
			content: ProductContent{
				Product: testComplianceProduct(t, catalog.ProductInput{
					SKU:         "LAMP-1",
					Title:       "Compact Desk Lamp",
					Description: "Compact desk lamp with adjustable brightness, warm light modes, and a stable base for focused work, reading, and bedside use.",
					Images:      []catalog.Image{{URL: "https://cdn.example.com/lamp.webp", Alt: "Compact desk lamp on a bedside table"}},
				}),
				Keywords:    []string{"desk lamp"},
				SEOTitle:    "Compact Desk Lamp With Adjustable Warm Light Modes for Focused Reading and Work",
				Meta:        "Compact desk lamp with adjustable brightness and warm light modes for focused work, reading, and bedside use.",
				SEOScoreMin: 90,
			},
		},
	}

	got := make([]complianceMatrixRow, 0, len(cases))
	for _, tt := range cases {
		result := engine.Evaluate(context.Background(), tt.content)
		got = append(got, complianceMatrixRow{
			Name:     tt.name,
			Pass:     result.Pass,
			Score:    result.Score,
			Severity: result.Severity,
			RuleIDs:  result.RuleIDs,
			Reasons:  result.Reasons,
		})
	}

	assertComplianceGoldenJSONPayload(t, filepath.Join("testdata", "compliance_matrix.golden.json"), got)
}

func TestSeverityRankingIsStable(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		a    Severity
		b    Severity
		want Severity
	}{
		{name: "info warning", a: SeverityInfo, b: SeverityWarning, want: SeverityWarning},
		{name: "warning error", a: SeverityWarning, b: SeverityError, want: SeverityError},
		{name: "error critical", a: SeverityError, b: SeverityCritical, want: SeverityCritical},
		{name: "critical warning", a: SeverityCritical, b: SeverityWarning, want: SeverityCritical},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := maxSeverity(tt.a, tt.b); got != tt.want {
				t.Fatalf("maxSeverity(%q, %q) = %q, want %q", tt.a, tt.b, got, tt.want)
			}
		})
	}
}

func TestAggregateScoreStability(t *testing.T) {
	t.Parallel()

	got := aggregate([]RuleResult{
		{ID: "pass", Pass: true, Score: 100, Severity: SeverityInfo},
		{ID: "seo", Pass: false, Score: 75, Severity: SeverityError, Reasons: []string{"seo score below minimum"}},
		{ID: "legal", Pass: false, Score: 0, Severity: SeverityCritical, Reasons: []string{"legal disclaimer is required"}},
	})

	if got.Pass {
		t.Fatal("aggregate passed, want fail")
	}
	if got.Score != 58 {
		t.Fatalf("score = %d, want stable integer average 58", got.Score)
	}
	if got.Severity != SeverityCritical {
		t.Fatalf("severity = %q, want critical", got.Severity)
	}
	if len(got.RuleIDs) != 2 || got.RuleIDs[0] != "seo" || got.RuleIDs[1] != "legal" {
		t.Fatalf("rule IDs = %#v, want failed rule order preserved", got.RuleIDs)
	}
}

func TestEngineHonoursContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	engine := NewEngine(DefaultRules())

	got := engine.Evaluate(ctx, ProductContent{})
	if got.Pass {
		t.Fatal("cancelled evaluation passed, want fail")
	}
	if !hasRuleID(got.RuleIDs, "engine_context") {
		t.Fatalf("rule IDs = %#v, want engine_context", got.RuleIDs)
	}
}

type complianceMatrixRow struct {
	Name     string   `json:"name"`
	Pass     bool     `json:"pass"`
	Score    int      `json:"score"`
	Severity Severity `json:"severity"`
	RuleIDs  []string `json:"rule_ids"`
	Reasons  []string `json:"reasons"`
}

func testComplianceProduct(t *testing.T, input catalog.ProductInput) catalog.Product {
	t.Helper()
	p, err := catalog.NewProduct(input)
	if err == nil {
		return p
	}
	return catalog.ReconstructProduct(catalog.ProductRecord{
		SKU:         input.SKU,
		Title:       input.Title,
		Slug:        input.Slug,
		Description: input.Description,
		Price:       input.Price,
		Stock:       input.Stock,
		Status:      input.Status,
		Images:      input.Images,
		Categories:  input.Categories,
	})
}

func hasRuleID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func assertComplianceGoldenJSONPayload(t *testing.T, goldenPath string, actualPayload any) {
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
