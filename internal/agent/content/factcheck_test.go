package content

import (
	"context"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/rag"
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

func TestFactCheckerClassifiesAccuracyFixtures(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		claim      string
		evidence   []rag.SearchResult
		wantStatus ClaimStatus
		wantPass   bool
		wantMin    float64
	}{
		{
			name:  "supported claim",
			claim: "Resistance Band Set includes five resistance levels.",
			evidence: []rag.SearchResult{
				{ChunkID: "chunk-supported", DocumentID: "doc-spec", Text: "Resistance Band Set includes five resistance levels for progressive workouts.", Source: "supplier-spec", Score: 0.93},
			},
			wantStatus: ClaimSupported,
			wantPass:   true,
			wantMin:    0.9,
		},
		{
			name:       "unsupported claim",
			claim:      "Resistance Band Set includes a lifetime biometric coaching subscription.",
			wantStatus: ClaimUnsupported,
			wantPass:   false,
		},
		{
			name:  "contradicted claim",
			claim: "Resistance Band Set includes seven resistance levels.",
			evidence: []rag.SearchResult{
				{ChunkID: "chunk-contradicted", DocumentID: "doc-spec", Text: "Resistance Band Set includes five resistance levels for progressive workouts.", Source: "supplier-spec", Score: 0.94},
			},
			wantStatus: ClaimContradicted,
			wantPass:   false,
			wantMin:    0.9,
		},
		{
			name:  "ambiguous claim",
			claim: "Resistance Band Set is suitable for professional studio rehab.",
			evidence: []rag.SearchResult{
				{ChunkID: "chunk-ambiguous", DocumentID: "doc-spec", Text: "Resistance Band Set supports progressive home workouts.", Source: "supplier-spec", Score: 0.46},
			},
			wantStatus: ClaimAmbiguous,
			wantPass:   false,
			wantMin:    0.4,
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			checker := NewFactChecker(fakeEvidenceSearcher{results: map[string][]rag.SearchResult{tt.claim: tt.evidence}}, FactCheckOptions{MinConfidence: 0.75, TopK: 3})

			result, err := checker.Check(context.Background(), GeneratedContent{Description: tt.claim})
			if err != nil {
				t.Fatalf("Check: %v", err)
			}
			if result.Pass != tt.wantPass {
				t.Fatalf("pass = %v, want %v: %+v", result.Pass, tt.wantPass, result)
			}
			if len(result.Claims) != 1 {
				t.Fatalf("claims = %d, want 1: %+v", len(result.Claims), result.Claims)
			}
			if result.Claims[0].Status != tt.wantStatus {
				t.Fatalf("status = %q, want %q: %+v", result.Claims[0].Status, tt.wantStatus, result.Claims[0])
			}
			if result.Claims[0].Confidence < tt.wantMin {
				t.Fatalf("confidence = %.2f, want >= %.2f", result.Claims[0].Confidence, tt.wantMin)
			}
			if !tt.wantPass && len(result.Issues) == 0 {
				t.Fatalf("expected issue for non-passing fixture: %+v", result)
			}
		})
	}
}

func TestFactCheckerThresholdBoundary(t *testing.T) {
	t.Parallel()

	const claim = "Resistance Band Set includes five resistance levels."
	searcher := fakeEvidenceSearcher{results: map[string][]rag.SearchResult{
		claim: {
			{ChunkID: "chunk-threshold", DocumentID: "doc-spec", Text: "Supplier spec for resistance accessories.", Source: "supplier-spec", Score: 0.75},
		},
	}}
	checker := NewFactChecker(searcher, FactCheckOptions{MinConfidence: 0.75, TopK: 3})

	result, err := checker.Check(context.Background(), GeneratedContent{Description: claim})
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !result.Pass || result.Claims[0].Status != ClaimSupported {
		t.Fatalf("threshold boundary should pass: %+v", result)
	}
}

type fakeEvidenceSearcher struct {
	results map[string][]rag.SearchResult
}

func (f fakeEvidenceSearcher) SearchText(_ context.Context, query string, _ int) ([]rag.SearchResult, error) {
	return append([]rag.SearchResult(nil), f.results[query]...), nil
}
