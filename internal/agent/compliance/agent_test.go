package compliance

import (
	"context"
	"testing"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
	contentagent "github.com/nfsarch33/helixon-ec/internal/agent/content"
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

func TestComplianceDescriptorAdvertisesQualityGateCapability(t *testing.T) {
	t.Parallel()

	descriptor := NewAgent().Descriptor()
	if descriptor.ID != "compliance" || descriptor.Name == "" {
		t.Fatalf("descriptor = %+v", descriptor)
	}
	if !containsReason(descriptor.Capabilities, "quality_gate") {
		t.Fatalf("capabilities = %v, want quality_gate", descriptor.Capabilities)
	}
}

func TestComplianceRunReturnsStructuredPayloadForScheduler(t *testing.T) {
	t.Parallel()

	agent := NewAgent()
	result, err := agent.Run(context.Background(), orchestrator.Task{Payload: map[string]any{
		"product": map[string]any{"id": "p1", "title": "Foam Roller"},
		"output": map[string]any{
			"description":      "Foam Roller supports simple recovery routines at home.",
			"seo_title":        "Foam Roller",
			"meta_description": "Foam Roller for recovery and mobility routines.",
		},
		"style":     "professional",
		"max_words": 80,
		"keywords":  []string{"foam roller"},
	}})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if result.Payload["pass"] != true {
		t.Fatalf("payload = %#v, want passing compliance result", result.Payload)
	}
	if _, ok := result.Payload["evaluation"].(map[string]any); !ok {
		t.Fatalf("payload missing normalized evaluation: %#v", result.Payload)
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
