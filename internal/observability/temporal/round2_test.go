package temporal

import (
	"context"
	"strings"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	"go.temporal.io/sdk/activity"
	"go.temporal.io/sdk/interceptor"
	"go.temporal.io/sdk/workflow"

	"github.com/nfsarch33/agentic-ecommerce/internal/observability/otelprovider"
)

// fakeWorkflowContext is the minimal workflow.Context (the SDK type is an
// interface mirroring context.Context) so the workflow-side half of the
// propagator can be driven without a live worker.
type fakeWorkflowContext struct{ values map[any]any }

func (c *fakeWorkflowContext) Deadline() (time.Time, bool) { return time.Time{}, false }
func (c *fakeWorkflowContext) Done() workflow.Channel      { return nil }
func (c *fakeWorkflowContext) Err() error                  { return nil }
func (c *fakeWorkflowContext) Value(k any) any             { return c.values[k] }

// Round 2, the bridge row: the propagator the starter (mc-api) and the
// worker both configure must carry tracecontext AND baggage across the
// Temporal header in both directions — HTTP edge -> run header ->
// activity context, and workflow values -> outbound headers.
// MUTANT: make Extract ignore the header map and both trace-id and
// baggage assertions fail.
func TestContextPropagatorCarriesTraceAndBaggage(t *testing.T) {
	prev := otel.GetTextMapPropagator()
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	defer otel.SetTextMapPropagator(prev)

	// A real span for a real trace id, plus one baggage member.
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	_, span := tp.Tracer("test").Start(context.Background(), "approve")
	defer span.End()

	member, err := baggage.NewMember("shop", "lane-b")
	if err != nil {
		t.Fatal(err)
	}
	bag, err := baggage.New(member)
	if err != nil {
		t.Fatal(err)
	}
	ctx := baggage.ContextWithBaggage(trace.ContextWithSpanContext(context.Background(), span.SpanContext()), bag)

	p := ContextPropagator()

	// Starter side: Go context -> Temporal header.
	hdr := headerMap{}
	if err := p.Inject(ctx, hdr); err != nil {
		t.Fatalf("Inject: %v", err)
	}
	if _, ok := hdr.Get("traceparent"); !ok {
		t.Fatalf("traceparent missing from the run header: %v", hdr)
	}

	// Worker side: header -> activity context.
	got, err := p.Extract(context.Background(), hdr)
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if sc := trace.SpanContextFromContext(got); sc.TraceID() != span.SpanContext().TraceID() {
		t.Fatalf("trace id did not cross the bridge: got %s want %s", sc.TraceID(), span.SpanContext().TraceID())
	}
	if m := baggage.FromContext(got).Member("shop"); m.Key() != "shop" || m.Value() != "lane-b" {
		t.Fatal("baggage did not cross the bridge")
	}

	// Workflow half: header -> workflow values -> outbound header.
	wctx, err := p.ExtractToWorkflow(&fakeWorkflowContext{values: map[any]any{}}, hdr)
	if err != nil {
		t.Fatalf("ExtractToWorkflow: %v", err)
	}
	out := headerMap{}
	if err := p.InjectFromWorkflow(wctx, out); err != nil {
		t.Fatalf("InjectFromWorkflow: %v", err)
	}
	if tp, ok := out.Get("traceparent"); !ok || tp == nil {
		t.Fatalf("workflow->activity replay lost traceparent: %v", out)
	}
	if bg, ok := out.Get("baggage"); !ok || bg == nil {
		t.Fatalf("workflow->activity replay lost baggage: %v", out)
	}
}

// Round 2, the id wiring row: activity spans carry the REAL workflow
// identity (workflow.name + run.id, allow-listed at export) read from
// the activity context; a context without activity info attaches
// nothing and never panics.
// MUTANT: drop the activityInfo block in ExecuteActivity and the
// identity assertions fail (attrs empty).
func TestActivitySpanCarriesWorkflowIdentity(t *testing.T) {
	old := activityInfo
	defer func() { activityInfo = old }()

	activityInfo = func(context.Context) activity.Info {
		return activity.Info{
			WorkflowExecution: workflow.Execution{RunID: "run-7"},
			WorkflowType:      &workflow.Type{Name: "ProductPublish"},
		}
	}

	rec := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(otelprovider.NewAllowListProcessor(rec)),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	next := &capturingActivityNext{}
	in := &interceptor.ExecuteActivityInput{Args: []interface{}{"PublishProduct"}}
	if _, err := (&activityInterceptor{tracer: tp.Tracer("t"), ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next}}).ExecuteActivity(context.Background(), in); err != nil {
		t.Fatalf("ExecuteActivity: %v", err)
	}

	spans := rec.Ended()
	if len(spans) != 1 {
		t.Fatalf("recorder saw %d spans, want 1", len(spans))
	}
	dump := ""
	for _, kv := range spans[0].Attributes() {
		dump += string(kv.Key) + "=" + kv.Value.Emit() + "\n"
	}
	for _, want := range []string{"workflow.name=ProductPublish", "run.id=run-7", "temporal.activity.type=PublishProduct"} {
		if !strings.Contains(dump, want) {
			t.Fatalf("activity identity %s missing from the exported span:\n%s", want, dump)
		}
	}

	// No activity info in the context: nothing attached, no panic.
	activityInfo = func(context.Context) activity.Info { return activity.Info{} }
	rec2 := tracetest.NewSpanRecorder()
	tp2 := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(otelprovider.NewAllowListProcessor(rec2)),
	)
	defer func() { _ = tp2.Shutdown(context.Background()) }()
	if _, err := (&activityInterceptor{tracer: tp2.Tracer("t"), ActivityInboundInterceptorBase: interceptor.ActivityInboundInterceptorBase{Next: next}}).ExecuteActivity(context.Background(), in); err != nil {
		t.Fatalf("ExecuteActivity (no info): %v", err)
	}
	for _, kv := range rec2.Ended()[0].Attributes() {
		if string(kv.Key) == "workflow.name" || string(kv.Key) == "run.id" {
			t.Fatalf("identity attached without activity info: %+v", kv)
		}
	}
}

type capturingActivityNext struct {
	interceptor.ActivityInboundInterceptorBase
}

func (c *capturingActivityNext) ExecuteActivity(context.Context, *interceptor.ExecuteActivityInput) (interface{}, error) {
	return "ok", nil
}
