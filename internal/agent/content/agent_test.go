package content_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/agent/content"
	"github.com/nfsarch33/helixon-ec/internal/port"
)

type fakeGenerator struct {
	response port.AICompletionResponse
	err      error
	captured []port.AICompletionRequest
}

func TestAgentGenerateFallsBackForPlainTextResponse(t *testing.T) {
	t.Parallel()

	generator := &fakeGenerator{response: port.AICompletionResponse{
		Content:    "Resistance Band Set helps shoppers train at home with compact progressive resistance.",
		TokensUsed: 21,
	}}
	agent := content.NewAgent(generator)

	result, err := agent.Generate(context.Background(), content.GenerateRequest{
		Product: content.ProductInfo{
			ID:    "b1000000-0000-0000-0000-000000000001",
			Title: "Resistance Band Set",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}
	if result.Description == "" || result.SEOTitle != "Resistance Band Set" || result.MetaDescription == "" {
		t.Fatalf("fallback result = %+v", result)
	}
}

func TestAgentGenerateRejectsMissingProductTitle(t *testing.T) {
	t.Parallel()

	agent := content.NewAgent(&fakeGenerator{})
	_, err := agent.Generate(context.Background(), content.GenerateRequest{})
	if !errors.Is(err, content.ErrMissingProduct) {
		t.Fatalf("err = %v, want ErrMissingProduct", err)
	}
}

func TestAgentGenerateRequiresGenerator(t *testing.T) {
	t.Parallel()

	agent := content.NewAgent(nil)
	_, err := agent.Generate(context.Background(), content.GenerateRequest{
		Product: content.ProductInfo{Title: "Resistance Band Set"},
	})
	if !errors.Is(err, content.ErrMissingGenerator) {
		t.Fatalf("err = %v, want ErrMissingGenerator", err)
	}
}

func (f *fakeGenerator) Complete(_ context.Context, req port.AICompletionRequest) (port.AICompletionResponse, error) {
	f.captured = append(f.captured, req)
	if f.err != nil {
		return port.AICompletionResponse{}, f.err
	}
	return f.response, nil
}

func TestAgentGenerateBuildsMigratedDescriberPrompt(t *testing.T) {
	t.Parallel()

	generator := &fakeGenerator{response: port.AICompletionResponse{
		Content:    `{"description":"Resistance Band Set supports progressive home workouts with five tension levels and compact storage.","seo_title":"Resistance Band Set for Home Workouts","meta_description":"Build strength anywhere with a compact resistance band set for progressive home workouts."}`,
		TokensUsed: 92,
	}}
	agent := content.NewAgent(generator)

	result, err := agent.Generate(context.Background(), content.GenerateRequest{
		Product: content.ProductInfo{
			ID:          "b1000000-0000-0000-0000-000000000001",
			SKU:         "RB-SET-5",
			Title:       "Resistance Band Set",
			Description: "Five tension levels for home training.",
			PriceAmount: 4995,
			Currency:    "AUD",
			Stock:       120,
			Categories:  []string{"Fitness", "Strength"},
		},
		Style:    content.StyleCasual,
		Language: "en-AU",
		MaxWords: 80,
		Keywords: []string{
			"resistance band set",
			"home workouts",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if result.Description == "" || result.SEOTitle == "" || result.MetaDescription == "" {
		t.Fatalf("missing generated fields: %+v", result)
	}
	if result.TokensUsed != 92 {
		t.Fatalf("TokensUsed = %d, want 92", result.TokensUsed)
	}
	if len(generator.captured) != 1 {
		t.Fatalf("captured requests = %d, want 1", len(generator.captured))
	}
	req := generator.captured[0]
	if len(req.Messages) != 2 {
		t.Fatalf("messages = %d, want 2", len(req.Messages))
	}
	if !strings.Contains(req.Messages[0].Content, "friendly e-commerce copywriter") {
		t.Fatalf("system prompt did not preserve casual describer style: %q", req.Messages[0].Content)
	}
	userPrompt := req.Messages[1].Content
	for _, want := range []string{"Resistance Band Set", "RB-SET-5", "$49.95 AUD", "Fitness, Strength", "80 words", "seo_title", "meta_description"} {
		if !strings.Contains(userPrompt, want) {
			t.Fatalf("user prompt missing %q:\n%s", want, userPrompt)
		}
	}
}

func TestAgentGenerateEvaluatesOutputQuality(t *testing.T) {
	t.Parallel()

	generator := &fakeGenerator{response: port.AICompletionResponse{
		Content:    `{"description":"Resistance Band Set makes home workouts simple. This resistance band set includes five tension levels for warm ups, strength work, and travel training.","seo_title":"Resistance Band Set for Home Workouts","meta_description":"Shop a compact resistance band set for home workouts, warm ups, and travel strength training."}`,
		TokensUsed: 120,
	}}
	agent := content.NewAgent(generator)

	result, err := agent.Generate(context.Background(), content.GenerateRequest{
		Product: content.ProductInfo{
			ID:          "b1000000-0000-0000-0000-000000000001",
			SKU:         "RB-SET-5",
			Title:       "Resistance Band Set",
			PriceAmount: 4995,
			Currency:    "AUD",
		},
		Style:    content.StyleProfessional,
		MaxWords: 55,
		Keywords: []string{
			"resistance band set",
			"home workouts",
		},
	})
	if err != nil {
		t.Fatalf("Generate returned error: %v", err)
	}

	if !result.Evaluation.Pass {
		t.Fatalf("evaluation should pass: %+v", result.Evaluation)
	}
	if result.Evaluation.Score < 75 {
		t.Fatalf("Score = %d, want >= 75", result.Evaluation.Score)
	}
	if density := result.Evaluation.KeywordDensity["resistance band set"]; density <= 0 {
		t.Fatalf("keyword density = %.2f, want positive", density)
	}
}
