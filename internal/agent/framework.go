package agent

import "context"

// LifecycleAgent captures the plan/execute/evaluate/report flow used by
// specialized agents such as the content generator.
type LifecycleAgent[Input any, Plan any, Result any, Evaluation any, Report any] interface {
	Plan(ctx context.Context, input Input) (Plan, error)
	Execute(ctx context.Context, plan Plan) (Result, error)
	Evaluate(ctx context.Context, result Result) (Evaluation, error)
	Report(ctx context.Context, result Result, evaluation Evaluation) (Report, error)
}
