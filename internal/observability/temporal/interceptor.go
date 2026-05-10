// Package temporal provides OTel-instrumented Temporal worker
// interceptors. Extracted from internal/observability/otel in v4.10.0
// to break a sentrux structural cycle with internal/workflow.
package temporal

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"
)

// TemporalInterceptor returns a Temporal worker interceptor that
// creates spans for workflow executions and activity invocations,
// linking them with parent-child relationships for end-to-end trace
// visibility.
func TemporalInterceptor() interceptor.WorkerInterceptor {
	return &temporalTracer{tracer: otel.Tracer("github.com/nfsarch33/agentic-ecommerce/internal/observability/temporal")}
}

type temporalTracer struct {
	interceptor.WorkerInterceptorBase
	tracer trace.Tracer
}

func (t *temporalTracer) InterceptActivity(ctx context.Context, next interceptor.ActivityInboundInterceptor) interceptor.ActivityInboundInterceptor {
	return &activityInterceptor{
		ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next},
		tracer:                         t.tracer,
	}
}

func (t *temporalTracer) InterceptWorkflow(ctx workflow.Context, next interceptor.WorkflowInboundInterceptor) interceptor.WorkflowInboundInterceptor {
	return &workflowInterceptor{
		WorkflowInboundInterceptorBase: interceptor.WorkflowInboundInterceptorBase{Next: next},
		tracer:                         t.tracer,
	}
}

type activityInterceptor struct {
	interceptor.ActivityInboundInterceptorBase
	tracer trace.Tracer
}

func (a *activityInterceptor) ExecuteActivity(ctx context.Context, in *interceptor.ExecuteActivityInput) (interface{}, error) {
	ctx, span := a.tracer.Start(ctx, "temporal.activity."+in.Args[0].(string),
		trace.WithSpanKind(trace.SpanKindInternal),
		trace.WithAttributes(attribute.String("temporal.activity.type", activityTypeName(in))),
	)
	defer span.End()

	result, err := a.Next.ExecuteActivity(ctx, in)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(2, err.Error())
	}
	return result, err
}

type workflowInterceptor struct {
	interceptor.WorkflowInboundInterceptorBase
	tracer trace.Tracer
}

func (w *workflowInterceptor) ExecuteWorkflow(ctx workflow.Context, in *interceptor.ExecuteWorkflowInput) (interface{}, error) {
	return w.Next.ExecuteWorkflow(ctx, in)
}

func activityTypeName(in *interceptor.ExecuteActivityInput) string {
	if in == nil || len(in.Args) == 0 {
		return "unknown"
	}
	if s, ok := in.Args[0].(string); ok {
		return s
	}
	return "unknown"
}
