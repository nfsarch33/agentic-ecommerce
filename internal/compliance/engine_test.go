package compliance

import (
	"context"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/domain/catalog"
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
