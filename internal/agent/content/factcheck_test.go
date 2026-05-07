package content

import (
	"context"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/rag"
)

func TestExtractClaimsFindsDeterministicProductClaims(t *testing.T) {
	t.Parallel()

	claims := ExtractClaims("Resistance Band Set includes five resistance levels. It is made from natural latex. Shop now for fast delivery!")
	if len(claims) != 2 {
		t.Fatalf("claims = %d, want 2: %+v", len(claims), claims)
	}
	if claims[0].Text != "Resistance Band Set includes five resistance levels." {
		t.Fatalf("first claim = %+v", claims[0])
	}
	if claims[1].Text != "It is made from natural latex." {
		t.Fatalf("second claim = %+v", claims[1])
	}
}

func TestFactCheckerScoresSupportedClaimsWithEvidence(t *testing.T) {
	t.Parallel()

	searcher := fakeEvidenceSearcher{results: map[string][]rag.SearchResult{
		"Resistance Band Set includes five resistance levels.": {
			{ChunkID: "chunk-1", DocumentID: "doc-1", Text: "The Resistance Band Set includes five resistance levels for progressive workouts.", Source: "supplier-spec", Score: 0.92},
		},
		"It is made from natural latex.": {
			{ChunkID: "chunk-2", DocumentID: "doc-1", Text: "Bands are made from natural latex.", Source: "supplier-spec", Score: 0.87},
		},
	}}
	checker := NewFactChecker(searcher, FactCheckOptions{MinConfidence: 0.75, TopK: 3})

	result, err := checker.Check(context.Background(), GeneratedContent{
		Description: "Resistance Band Set includes five resistance levels. It is made from natural latex.",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Pass {
		t.Fatalf("result should pass: %+v", result)
	}
	if result.Confidence < 0.85 {
		t.Fatalf("confidence = %.2f, want >= 0.85", result.Confidence)
	}
	if len(result.Claims) != 2 || result.Claims[0].Evidence[0].DocumentID != "doc-1" {
		t.Fatalf("claims = %+v", result.Claims)
	}
}

func TestFactCheckerFlagsUnsupportedClaims(t *testing.T) {
	t.Parallel()

	checker := NewFactChecker(fakeEvidenceSearcher{}, FactCheckOptions{MinConfidence: 0.75, TopK: 3})
	result, err := checker.Check(context.Background(), GeneratedContent{
		Description: "Resistance Band Set includes seven resistance levels.",
	})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if result.Pass {
		t.Fatalf("unsupported claim should fail: %+v", result)
	}
	if len(result.Claims) != 1 || result.Claims[0].Status != ClaimUnsupported {
		t.Fatalf("claims = %+v", result.Claims)
	}
	if len(result.Issues) == 0 {
		t.Fatalf("expected issues: %+v", result)
	}
}

type fakeEvidenceSearcher struct {
	results map[string][]rag.SearchResult
}

func (f fakeEvidenceSearcher) SearchText(_ context.Context, query string, _ int) ([]rag.SearchResult, error) {
	return append([]rag.SearchResult(nil), f.results[query]...), nil
}
