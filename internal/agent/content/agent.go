package content

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/nfsarch33/helixon-ec/internal/port"
)

var (
	ErrMissingGenerator = errors.New("missing ai text generator")
	ErrMissingProduct   = errors.New("missing product title")
	ErrEmptyGeneration  = errors.New("empty ai generation")
)

type Agent struct {
	generator port.AITextGenerator
	evaluator Evaluator
}

func NewAgent(generator port.AITextGenerator) *Agent {
	return &Agent{generator: generator, evaluator: NewEvaluator()}
}

func (a *Agent) Generate(ctx context.Context, req GenerateRequest) (GenerateResult, error) {
	plan, err := a.Plan(ctx, req)
	if err != nil {
		return GenerateResult{}, err
	}
	result, err := a.Execute(ctx, plan)
	if err != nil {
		return GenerateResult{}, err
	}
	evaluation, err := a.Evaluate(ctx, result)
	if err != nil {
		return GenerateResult{}, err
	}
	result.Evaluation = evaluation
	report, err := a.Report(ctx, result, evaluation)
	if err != nil {
		return GenerateResult{}, err
	}
	result.TokensUsed = report.TokensUsed
	return result, nil
}

func (a *Agent) Plan(_ context.Context, req GenerateRequest) (Plan, error) {
	if strings.TrimSpace(req.Product.Title) == "" {
		return Plan{}, ErrMissingProduct
	}
	if req.Style == "" {
		req.Style = StyleProfessional
	}
	if req.Language == "" {
		req.Language = "en-AU"
	}
	if req.MaxWords == 0 {
		req.MaxWords = 120
	}
	return Plan{
		Goal:        "Generate product content for " + req.Product.Title,
		System:      styleSystemPrompt(req.Style),
		User:        buildUserPrompt(req),
		Request:     req,
		MaxTokens:   700,
		Temperature: 0.4,
	}, nil
}

func (a *Agent) Execute(ctx context.Context, plan Plan) (GenerateResult, error) {
	if a.generator == nil {
		return GenerateResult{}, ErrMissingGenerator
	}
	resp, err := a.generator.Complete(ctx, port.AICompletionRequest{
		Messages: []port.AIMessage{
			{Role: "system", Content: plan.System},
			{Role: "user", Content: plan.User},
		},
		Temperature: &plan.Temperature,
		MaxTokens:   &plan.MaxTokens,
	})
	if err != nil {
		return GenerateResult{}, fmt.Errorf("generate content: %w", err)
	}
	content, err := parseGeneratedContent(resp.Content, plan.Request.Product.Title)
	if err != nil {
		return GenerateResult{}, err
	}
	return GenerateResult{
		GeneratedContent: content,
		Request:          plan.Request,
		TokensUsed:       resp.TokensUsed,
	}, nil
}

func (a *Agent) Evaluate(_ context.Context, result GenerateResult) (Evaluation, error) {
	return a.evaluateForRequest(result.Request, result), nil
}

func (a *Agent) Report(_ context.Context, result GenerateResult, evaluation Evaluation) (Report, error) {
	return Report{
		ProductID:  result.Request.Product.ID,
		Score:      evaluation.Score,
		Pass:       evaluation.Pass,
		TokensUsed: result.TokensUsed,
	}, nil
}

func (a *Agent) evaluateForRequest(req GenerateRequest, result GenerateResult) Evaluation {
	return a.evaluator.Evaluate(EvaluationInput{
		Product:  req.Product,
		Output:   result.GeneratedContent,
		Style:    req.Style,
		MaxWords: req.MaxWords,
		Keywords: req.Keywords,
	})
}

func parseGeneratedContent(raw string, productTitle string) (GeneratedContent, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return GeneratedContent{}, ErrEmptyGeneration
	}
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var out GeneratedContent
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		out.Description = strings.TrimSpace(out.Description)
		out.SEOTitle = strings.TrimSpace(out.SEOTitle)
		out.MetaDescription = strings.TrimSpace(out.MetaDescription)
		if out.Description == "" {
			return GeneratedContent{}, ErrEmptyGeneration
		}
		if out.SEOTitle == "" {
			out.SEOTitle = deriveSEOTitle(productTitle)
		}
		if out.MetaDescription == "" {
			out.MetaDescription = deriveMetaDescription(out.Description)
		}
		return out, nil
	}

	return GeneratedContent{
		Description:     raw,
		SEOTitle:        deriveSEOTitle(productTitle),
		MetaDescription: deriveMetaDescription(raw),
	}, nil
}

func deriveSEOTitle(productTitle string) string {
	if productTitle == "" {
		return "Product Description"
	}
	if len(productTitle) <= 60 {
		return productTitle
	}
	return strings.TrimSpace(productTitle[:60])
}

func deriveMetaDescription(description string) string {
	words := strings.Fields(description)
	if len(words) == 0 {
		return ""
	}
	meta := strings.Join(words, " ")
	if len(meta) <= 160 {
		return meta
	}
	return strings.TrimSpace(meta[:157]) + "..."
}
