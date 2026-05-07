package agent

import "context"

// Agent captures the minimal lifecycle shared by backend agents.
type Agent[Input any, Plan any, Result any, Evaluation any, Report any] interface {
	Plan(ctx context.Context, input Input) (Plan, error)
	Execute(ctx context.Context, plan Plan) (Result, error)
	Evaluate(ctx context.Context, result Result) (Evaluation, error)
	Report(ctx context.Context, result Result, evaluation Evaluation) (Report, error)
}
