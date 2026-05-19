package compliance

import (
	"context"
	"encoding/json"
	"errors"

	orchestrator "github.com/nfsarch33/helixon-ec/internal/agent"
	contentagent "github.com/nfsarch33/helixon-ec/internal/agent/content"
)

var ErrMissingContent = errors.New("generated content description is required")

type Agent struct {
	evaluator contentagent.Evaluator
}

type Request struct {
	Product  contentagent.ProductInfo      `json:"product"`
	Output   contentagent.GeneratedContent `json:"output"`
	Style    contentagent.Style            `json:"style"`
	MaxWords int                           `json:"max_words"`
	Keywords []string                      `json:"keywords"`
}

type Result struct {
	Pass       bool                    `json:"pass"`
	Score      int                     `json:"score"`
	Reasons    []string                `json:"reasons"`
	Evaluation contentagent.Evaluation `json:"evaluation"`
}

func NewAgent() *Agent {
	return &Agent{evaluator: contentagent.NewEvaluator()}
}

func (a *Agent) Descriptor() orchestrator.Descriptor {
	return orchestrator.Descriptor{
		ID:           "compliance",
		Name:         "Compliance Agent",
		Description:  "Runs structured content compliance checks and returns pass/fail reasons.",
		Capabilities: []string{"content_policy", "quality_gate"},
	}
}

func (a *Agent) Run(ctx context.Context, task orchestrator.Task) (orchestrator.RunResult, error) {
	var req Request
	if err := decodePayload(task.Payload, &req); err != nil {
		return orchestrator.RunResult{}, err
	}
	result, err := a.Check(ctx, req)
	if err != nil {
		return orchestrator.RunResult{}, err
	}
	return orchestrator.RunResult{Payload: mustMap(result)}, nil
}

func (a *Agent) Check(ctx context.Context, req Request) (Result, error) {
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	if req.Output.Description == "" {
		return Result{}, ErrMissingContent
	}
	if req.Style == "" {
		req.Style = contentagent.StyleProfessional
	}
	evaluation := a.evaluator.Evaluate(contentagent.EvaluationInput{
		Product:  req.Product,
		Output:   req.Output,
		Style:    req.Style,
		MaxWords: req.MaxWords,
		Keywords: req.Keywords,
	})
	reasons := failureReasons(evaluation)
	return Result{
		Pass:       evaluation.Pass,
		Score:      evaluation.Score,
		Reasons:    reasons,
		Evaluation: normalizeEvaluation(evaluation),
	}, nil
}

func failureReasons(e contentagent.Evaluation) []string {
	reasons := make([]string, 0)
	if !e.Length.WithinLimit {
		reasons = append(reasons, "content exceeds max words")
	}
	if !e.Tone.Pass {
		reasons = append(reasons, e.Tone.Issues...)
	}
	reasons = append(reasons, e.FactualIssues...)
	for keyword, density := range e.KeywordDensity {
		if density == 0 {
			reasons = append(reasons, "missing keyword: "+keyword)
		}
	}
	return reasons
}

func normalizeEvaluation(e contentagent.Evaluation) contentagent.Evaluation {
	if e.KeywordDensity == nil {
		e.KeywordDensity = map[string]float64{}
	}
	if e.Tone.Issues == nil {
		e.Tone.Issues = []string{}
	}
	if e.FactualIssues == nil {
		e.FactualIssues = []string{}
	}
	return e
}

func decodePayload(payload map[string]any, out any) error {
	data, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, out)
}

func mustMap(value any) map[string]any {
	data, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		return map[string]any{}
	}
	return out
}
