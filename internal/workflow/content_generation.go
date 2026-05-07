package workflow

import (
	"context"
	"errors"
	"time"

	contentagent "github.com/nfsarch33/agentic-ecommerce/internal/agent/content"
	"go.temporal.io/sdk/temporal"
	temporalworkflow "go.temporal.io/sdk/workflow"
)

const (
	ContentGenerateActivity          = "content_generation.generate"
	ContentFactCheckActivity         = "content_generation.fact_check"
	ContentEvaluateActivity          = "content_generation.evaluate"
	RecordContentFactCheckActivity   = "content_generation.record_fact_check"
	ContentGenerationStatusGenerated = "generated"
	ContentGenerationStatusApproved  = "approved"
	ContentGenerationStatusRejected  = "rejected"
)

type ContentGenerationInput struct {
	Product     contentagent.ProductInfo     `json:"product"`
	Request     contentagent.GenerateRequest `json:"request"`
	RequestedBy string                       `json:"requested_by,omitempty"`
}

type ContentFactCheckActivityInput struct {
	ProductID string                        `json:"product_id"`
	Content   contentagent.GeneratedContent `json:"content"`
}

type ContentEvaluateActivityInput struct {
	Request contentagent.GenerateRequest `json:"request"`
	Result  contentagent.GenerateResult  `json:"result"`
}

type ContentGenerationResult struct {
	ProductID   string                        `json:"product_id"`
	Status      string                        `json:"status"`
	Approved    bool                          `json:"approved"`
	Content     contentagent.GeneratedContent `json:"content"`
	Evaluation  contentagent.Evaluation       `json:"evaluation"`
	FactCheck   contentagent.FactCheckResult  `json:"fact_check"`
	TokensUsed  int                           `json:"tokens_used"`
	RequestedBy string                        `json:"requested_by,omitempty"`
}

type ContentGenerationActivityDeps struct {
	Generator interface {
		Generate(context.Context, contentagent.GenerateRequest) (contentagent.GenerateResult, error)
	}
	FactChecker interface {
		Check(context.Context, contentagent.GeneratedContent) (contentagent.FactCheckResult, error)
	}
	Recorder interface {
		RecordContentFactCheck(context.Context, ContentGenerationResult) error
	}
}

type ContentGenerationActivities struct {
	generator interface {
		Generate(context.Context, contentagent.GenerateRequest) (contentagent.GenerateResult, error)
	}
	factChecker interface {
		Check(context.Context, contentagent.GeneratedContent) (contentagent.FactCheckResult, error)
	}
	recorder interface {
		RecordContentFactCheck(context.Context, ContentGenerationResult) error
	}
}

func NewContentGenerationActivities(deps ContentGenerationActivityDeps) *ContentGenerationActivities {
	return &ContentGenerationActivities{generator: deps.Generator, factChecker: deps.FactChecker, recorder: deps.Recorder}
}

func ContentGenerationWorkflow(ctx temporalworkflow.Context, input ContentGenerationInput) (ContentGenerationResult, error) {
	activityOptions := temporalworkflow.ActivityOptions{
		StartToCloseTimeout: time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval:    time.Second,
			BackoffCoefficient: 2,
			MaximumInterval:    30 * time.Second,
			MaximumAttempts:    3,
		},
	}
	ctx = temporalworkflow.WithActivityOptions(ctx, activityOptions)

	state := ContentGenerationResult{ProductID: input.Product.ID, Status: ContentGenerationStatusGenerated, RequestedBy: input.RequestedBy}
	var generated contentagent.GenerateResult
	if err := temporalworkflow.ExecuteActivity(ctx, ContentGenerateActivity, input).Get(ctx, &generated); err != nil {
		return state, err
	}
	state.Content = generated.GeneratedContent
	state.Evaluation = generated.Evaluation
	state.TokensUsed = generated.TokensUsed

	if err := temporalworkflow.ExecuteActivity(ctx, ContentFactCheckActivity, ContentFactCheckActivityInput{
		ProductID: input.Product.ID,
		Content:   generated.GeneratedContent,
	}).Get(ctx, &state.FactCheck); err != nil {
		return state, err
	}
	if err := temporalworkflow.ExecuteActivity(ctx, ContentEvaluateActivity, ContentEvaluateActivityInput{
		Request: input.Request,
		Result:  generated,
	}).Get(ctx, &state.Evaluation); err != nil {
		return state, err
	}

	state.Approved = state.FactCheck.Pass && state.Evaluation.Pass
	if state.Approved {
		state.Status = ContentGenerationStatusApproved
	} else {
		state.Status = ContentGenerationStatusRejected
	}
	if err := temporalworkflow.ExecuteActivity(ctx, RecordContentFactCheckActivity, state).Get(ctx, nil); err != nil {
		return state, err
	}
	return state, nil
}

func (a *ContentGenerationActivities) GenerateContent(ctx context.Context, input ContentGenerationInput) (contentagent.GenerateResult, error) {
	if a.generator == nil {
		return contentagent.GenerateResult{}, errors.New("content generator is not configured")
	}
	req := input.Request
	if req.Product.Title == "" {
		req.Product = input.Product
	}
	return a.generator.Generate(ctx, req)
}

func (a *ContentGenerationActivities) FactCheckContent(ctx context.Context, input ContentFactCheckActivityInput) (contentagent.FactCheckResult, error) {
	if a.factChecker == nil {
		return contentagent.FactCheckResult{}, errors.New("fact checker is not configured")
	}
	result, err := a.factChecker.Check(ctx, input.Content)
	if err != nil {
		return contentagent.FactCheckResult{}, err
	}
	result.ProductID = input.ProductID
	return result, nil
}

func (a *ContentGenerationActivities) EvaluateContent(_ context.Context, input ContentEvaluateActivityInput) (contentagent.Evaluation, error) {
	evaluator := contentagent.NewEvaluator()
	return evaluator.Evaluate(contentagent.EvaluationInput{
		Product:  input.Request.Product,
		Output:   input.Result.GeneratedContent,
		Style:    input.Request.Style,
		MaxWords: input.Request.MaxWords,
		Keywords: input.Request.Keywords,
	}), nil
}

func (a *ContentGenerationActivities) RecordContentFactCheck(ctx context.Context, result ContentGenerationResult) error {
	if a.recorder == nil {
		return nil
	}
	return a.recorder.RecordContentFactCheck(ctx, result)
}
