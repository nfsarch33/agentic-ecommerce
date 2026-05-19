package workflow

import (
	"context"
	"testing"

	contentagent "github.com/nfsarch33/helixon-ec/internal/agent/content"
	"github.com/stretchr/testify/mock"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/testsuite"
)

func TestContentGenerationWorkflowApprovesFactCheckedContent(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerContentGenerationTestActivities(env)

	input := ContentGenerationInput{
		Product: contentagent.ProductInfo{ID: "product-1", Title: "Resistance Band Set"},
		Request: contentagent.GenerateRequest{
			Product:  contentagent.ProductInfo{ID: "product-1", Title: "Resistance Band Set"},
			MaxWords: 80,
		},
		RequestedBy: "operator@example.com",
	}
	generated := contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{Description: "Resistance Band Set includes five resistance levels."},
		Evaluation:       contentagent.Evaluation{Score: 90, Pass: true},
	}
	factCheck := contentagent.FactCheckResult{Pass: true, Confidence: 0.91}
	evaluation := contentagent.Evaluation{Score: 90, Pass: true}

	env.OnActivity(ContentGenerateActivity, mock.Anything, input).Return(generated, nil).Once()
	env.OnActivity(ContentFactCheckActivity, mock.Anything, ContentFactCheckActivityInput{
		ProductID: input.Product.ID,
		Content:   generated.GeneratedContent,
	}).Return(factCheck, nil).Once()
	env.OnActivity(ContentEvaluateActivity, mock.Anything, ContentEvaluateActivityInput{
		Request: input.Request,
		Result:  generated,
	}).Return(evaluation, nil).Once()
	env.OnActivity(RecordContentFactCheckActivity, mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ContentGenerationWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ContentGenerationResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ContentGenerationStatusApproved || !result.Approved {
		t.Fatalf("result = %+v, want approved", result)
	}
	if result.FactCheck.Confidence != 0.91 {
		t.Fatalf("fact check = %+v", result.FactCheck)
	}
}

func TestContentGenerationWorkflowRejectsLowFactCheckConfidence(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerContentGenerationTestActivities(env)

	input := ContentGenerationInput{
		Product: contentagent.ProductInfo{ID: "product-2", Title: "Foam Roller"},
		Request: contentagent.GenerateRequest{
			Product:  contentagent.ProductInfo{ID: "product-2", Title: "Foam Roller"},
			MaxWords: 80,
		},
	}
	generated := contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{Description: "Foam Roller is made from carbon fibre."},
		Evaluation:       contentagent.Evaluation{Score: 90, Pass: true},
	}
	factCheck := contentagent.FactCheckResult{Pass: false, Confidence: 0.2, Issues: []string{"unsupported claim"}}
	evaluation := contentagent.Evaluation{Score: 90, Pass: true}

	env.OnActivity(ContentGenerateActivity, mock.Anything, input).Return(generated, nil).Once()
	env.OnActivity(ContentFactCheckActivity, mock.Anything, ContentFactCheckActivityInput{
		ProductID: input.Product.ID,
		Content:   generated.GeneratedContent,
	}).Return(factCheck, nil).Once()
	env.OnActivity(ContentEvaluateActivity, mock.Anything, ContentEvaluateActivityInput{
		Request: input.Request,
		Result:  generated,
	}).Return(evaluation, nil).Once()
	env.OnActivity(RecordContentFactCheckActivity, mock.Anything, mock.Anything).Return(nil).Once()

	env.ExecuteWorkflow(ContentGenerationWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ContentGenerationResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ContentGenerationStatusRejected || result.Approved {
		t.Fatalf("result = %+v, want rejected", result)
	}
}

func TestContentGenerationWorkflowRejectsWhenEvaluationFailsAfterFactCheckPasses(t *testing.T) {
	t.Parallel()

	var suite testsuite.WorkflowTestSuite
	env := suite.NewTestWorkflowEnvironment()
	registerContentGenerationTestActivities(env)

	input := ContentGenerationInput{
		Product: contentagent.ProductInfo{ID: "product-3", Title: "Resistance Band Set"},
		Request: contentagent.GenerateRequest{
			Product:  contentagent.ProductInfo{ID: "product-3", Title: "Resistance Band Set"},
			MaxWords: 12,
		},
		RequestedBy: "qa@example.com",
	}
	generated := contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{Description: "Resistance Band Set includes five resistance levels for progressive workouts and ships with a detailed setup guide for home strength training."},
		Evaluation:       contentagent.Evaluation{Score: 90, Pass: true},
		TokensUsed:       22,
	}
	factCheck := contentagent.FactCheckResult{Pass: true, Confidence: 0.91}
	evaluation := contentagent.Evaluation{Score: 55, Pass: false, FactualIssues: []string{"too long"}}

	env.OnActivity(ContentGenerateActivity, mock.Anything, input).Return(generated, nil).Once()
	env.OnActivity(ContentFactCheckActivity, mock.Anything, ContentFactCheckActivityInput{
		ProductID: input.Product.ID,
		Content:   generated.GeneratedContent,
	}).Return(factCheck, nil).Once()
	env.OnActivity(ContentEvaluateActivity, mock.Anything, ContentEvaluateActivityInput{
		Request: input.Request,
		Result:  generated,
	}).Return(evaluation, nil).Once()
	env.OnActivity(RecordContentFactCheckActivity, mock.Anything, mock.MatchedBy(func(result ContentGenerationResult) bool {
		return result.ProductID == input.Product.ID && result.Status == ContentGenerationStatusRejected && !result.Approved && result.FactCheck.Pass
	})).Return(nil).Once()

	env.ExecuteWorkflow(ContentGenerationWorkflow, input)

	if err := env.GetWorkflowError(); err != nil {
		t.Fatalf("workflow error: %v", err)
	}
	var result ContentGenerationResult
	if err := env.GetWorkflowResult(&result); err != nil {
		t.Fatalf("workflow result: %v", err)
	}
	if result.Status != ContentGenerationStatusRejected || result.Approved || result.FactCheck.Confidence != 0.91 {
		t.Fatalf("result = %+v, want rejected with retained fact check", result)
	}
}

func TestContentGenerationActivitiesRunInjectedDependencies(t *testing.T) {
	t.Parallel()

	generator := &fakeWorkflowContentGenerator{result: contentagent.GenerateResult{
		GeneratedContent: contentagent.GeneratedContent{
			Description:     "Resistance Band Set includes five resistance levels.",
			SEOTitle:        "Resistance Band Set",
			MetaDescription: "Resistance Band Set includes five resistance levels.",
		},
		TokensUsed: 18,
	}}
	factChecker := &fakeWorkflowFactChecker{result: contentagent.FactCheckResult{Pass: true, Confidence: 0.88}}
	recorder := &fakeContentFactCheckRecorder{}
	activities := NewContentGenerationActivities(ContentGenerationActivityDeps{
		Generator:   generator,
		FactChecker: factChecker,
		Recorder:    recorder,
	})
	input := ContentGenerationInput{
		Product: contentagent.ProductInfo{ID: "product-activity", Title: "Resistance Band Set"},
		Request: contentagent.GenerateRequest{
			MaxWords: 50,
		},
		RequestedBy: "qa@example.com",
	}

	generated, err := activities.GenerateContent(context.Background(), input)
	if err != nil {
		t.Fatalf("GenerateContent: %v", err)
	}
	if generator.request.Product.Title != "Resistance Band Set" || generated.TokensUsed != 18 {
		t.Fatalf("generated = %+v request=%+v", generated, generator.request)
	}

	factCheck, err := activities.FactCheckContent(context.Background(), ContentFactCheckActivityInput{
		ProductID: input.Product.ID,
		Content:   generated.GeneratedContent,
	})
	if err != nil {
		t.Fatalf("FactCheckContent: %v", err)
	}
	if factCheck.ProductID != input.Product.ID || factCheck.Confidence != 0.88 {
		t.Fatalf("factCheck = %+v", factCheck)
	}

	evaluation, err := activities.EvaluateContent(context.Background(), ContentEvaluateActivityInput{
		Request: contentagent.GenerateRequest{Product: input.Product, MaxWords: 50},
		Result:  generated,
	})
	if err != nil {
		t.Fatalf("EvaluateContent: %v", err)
	}
	if !evaluation.Pass {
		t.Fatalf("evaluation = %+v, want pass", evaluation)
	}

	err = activities.RecordContentFactCheck(context.Background(), ContentGenerationResult{ProductID: input.Product.ID, FactCheck: factCheck})
	if err != nil {
		t.Fatalf("RecordContentFactCheck: %v", err)
	}
	if recorder.recorded.ProductID != input.Product.ID {
		t.Fatalf("recorded = %+v", recorder.recorded)
	}
}

func TestContentGenerationActivitiesRequireDependencies(t *testing.T) {
	t.Parallel()

	activities := NewContentGenerationActivities(ContentGenerationActivityDeps{})
	if _, err := activities.GenerateContent(context.Background(), ContentGenerationInput{}); err == nil {
		t.Fatal("expected missing generator error")
	}
	if _, err := activities.FactCheckContent(context.Background(), ContentFactCheckActivityInput{}); err == nil {
		t.Fatal("expected missing fact checker error")
	}
	if err := activities.RecordContentFactCheck(context.Background(), ContentGenerationResult{}); err != nil {
		t.Fatalf("nil recorder should be no-op: %v", err)
	}
}

func registerContentGenerationTestActivities(env *testsuite.TestWorkflowEnvironment) {
	env.RegisterActivityWithOptions(func(context.Context, ContentGenerationInput) (contentagent.GenerateResult, error) {
		return contentagent.GenerateResult{}, nil
	}, activity.RegisterOptions{Name: ContentGenerateActivity})
	env.RegisterActivityWithOptions(func(context.Context, ContentFactCheckActivityInput) (contentagent.FactCheckResult, error) {
		return contentagent.FactCheckResult{}, nil
	}, activity.RegisterOptions{Name: ContentFactCheckActivity})
	env.RegisterActivityWithOptions(func(context.Context, ContentEvaluateActivityInput) (contentagent.Evaluation, error) {
		return contentagent.Evaluation{}, nil
	}, activity.RegisterOptions{Name: ContentEvaluateActivity})
	env.RegisterActivityWithOptions(func(context.Context, ContentGenerationResult) error {
		return nil
	}, activity.RegisterOptions{Name: RecordContentFactCheckActivity})
}

type fakeWorkflowContentGenerator struct {
	result  contentagent.GenerateResult
	request contentagent.GenerateRequest
}

func (f *fakeWorkflowContentGenerator) Generate(_ context.Context, req contentagent.GenerateRequest) (contentagent.GenerateResult, error) {
	f.request = req
	return f.result, nil
}

type fakeWorkflowFactChecker struct {
	result contentagent.FactCheckResult
}

func (f *fakeWorkflowFactChecker) Check(_ context.Context, _ contentagent.GeneratedContent) (contentagent.FactCheckResult, error) {
	return f.result, nil
}

type fakeContentFactCheckRecorder struct {
	recorded ContentGenerationResult
}

func (f *fakeContentFactCheckRecorder) RecordContentFactCheck(_ context.Context, result ContentGenerationResult) error {
	f.recorded = result
	return nil
}
