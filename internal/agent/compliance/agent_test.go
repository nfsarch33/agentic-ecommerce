package compliance

import (
	"context"
	"testing"

	orchestrator "github.com/nfsarch33/agentic-ecommerce/internal/agent"
	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
)

func TestCheckWrapsContentEvaluatorWithPassReasons(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Check(context.Background(), Request{
		Product: contentagent.ProductInfo{ID: "p1", Title: "Foam Roller"},
		Output: contentagent.GeneratedContent{
			Description:     "Foam Roller supports simple recovery routines at home.",
			SEOTitle:        "Foam Roller",
			MetaDescription: "Foam Roller for recovery and mobility routines.",
		},
		Style:    contentagent.StyleProfessional,
		MaxWords: 80,
		Keywords: []string{"foam roller"},
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !result.Pass {
		t.Fatalf("expected pass, got %#v", result)
	}
	if len(result.Reasons) != 0 {
		t.Fatalf("pass reasons = %v, want empty", result.Reasons)
	}
}

func TestCheckReturnsStructuredFailureReasons(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Check(context.Background(), Request{
		Product: contentagent.ProductInfo{ID: "p1", Title: "Foam Roller"},
		Output: contentagent.GeneratedContent{
			Description:     "TODO cheap budget thing.",
			SEOTitle:        "Budget recovery",
			MetaDescription: "Lorem ipsum.",
		},
		Style:    contentagent.StyleLuxury,
		MaxWords: 3,
	})
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if result.Pass {
		t.Fatalf("expected failure, got %#v", result)
	}
	for _, want := range []string{"placeholder content present", "product title not referenced", "luxury tone uses discount language"} {
		if !containsReason(result.Reasons, want) {
			t.Fatalf("reasons %v missing %q", result.Reasons, want)
		}
	}
}

func TestComplianceRunRejectsMissingContent(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	_, err := agent.Run(context.Background(), orchestrator.Task{Payload: map[string]any{"product": map[string]any{"title": "Foam Roller"}}})
	if err == nil {
		t.Fatal("expected missing content error")
	}
}

func containsReason(reasons []string, want string) bool {
	for _, reason := range reasons {
		if reason == want {
			return true
		}
	}
	return false
}
