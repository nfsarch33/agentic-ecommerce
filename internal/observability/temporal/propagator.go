package temporal

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.temporal.io/api/common/v1"
	"go.temporal.io/sdk/converter"
	"go.temporal.io/sdk/workflow"
)

// ContextPropagator carries the process-global OTel propagator (W3C
// tracecontext + baggage) through Temporal's header map, so a trace
// started at the HTTP edge (the approve-to-publish request) survives the
// bridge into the workflow and its activities. Both processes must set
// the same propagator — otelprovider.Setup and mc-api's
// configureTelemetry install the identical composite, and this
// propagator reads it through otel.GetTextMapPropagator(), so the two
// sides cannot drift apart.
//
// The workflow side stores the raw header pairs as workflow.Context
// VALUES and replays them on outbound headers: pure data movement, no
// SDK calls, deterministic-workflow safe (spans are only ever created in
// activities and at the HTTP edge).
type otelContextPropagator struct{}

// ContextPropagator returns the OTel-backed Temporal context propagator.
func ContextPropagator() workflow.ContextPropagator { return otelContextPropagator{} }

type propagatorHeaders struct{}

// Inject runs on the workflow STARTER (the HTTP server): the current
// trace context and baggage go into the workflow's header.
func (otelContextPropagator) Inject(ctx context.Context, writer workflow.HeaderWriter) error {
	m := map[string]string{}
	otel.GetTextMapPropagator().Inject(ctx, stringMapCarrier{m: m})
	return writeHeader(writer, m)
}

// Extract runs at the activity and workflow-client edge: traceparent and
// baggage land in a real context.Context, where the activity
// interceptor's span picks up the parent.
func (otelContextPropagator) Extract(ctx context.Context, reader workflow.HeaderReader) (context.Context, error) {
	m, err := readHeader(reader)
	if err != nil || len(m) == 0 {
		return ctx, err
	}
	return otel.GetTextMapPropagator().Extract(ctx, stringMapCarrier{m: m}), nil
}

// InjectFromWorkflow runs in the worker when workflow code schedules an
// activity or child workflow: replay the headers stored by
// ExtractToWorkflow onto the outbound header.
func (otelContextPropagator) InjectFromWorkflow(ctx workflow.Context, writer workflow.HeaderWriter) error {
	if m, ok := ctx.Value(propagatorHeaders{}).(map[string]string); ok {
		return writeHeader(writer, m)
	}
	return nil
}

// ExtractToWorkflow stores the incoming header pairs as workflow context
// values for InjectFromWorkflow to replay.
func (otelContextPropagator) ExtractToWorkflow(ctx workflow.Context, reader workflow.HeaderReader) (workflow.Context, error) {
	m, err := readHeader(reader)
	if err != nil {
		return ctx, err
	}
	if len(m) == 0 {
		return ctx, nil
	}
	return workflow.WithValue(ctx, propagatorHeaders{}, m), nil
}

// readHeader pulls every header the global propagator declares
// (traceparent, tracestate, baggage) out of the Temporal header,
// payload-decoding each value.
func readHeader(reader workflow.HeaderReader) (map[string]string, error) {
	m := map[string]string{}
	dc := converter.GetDefaultDataConverter()
	for _, key := range otel.GetTextMapPropagator().Fields() {
		payload, ok := reader.Get(key)
		if !ok || payload == nil {
			continue
		}
		var v string
		if err := dc.FromPayload(payload, &v); err != nil {
			return nil, err
		}
		if v != "" {
			m[key] = v
		}
	}
	return m, nil
}

func writeHeader(writer workflow.HeaderWriter, m map[string]string) error {
	dc := converter.GetDefaultDataConverter()
	for k, v := range m {
		payload, err := dc.ToPayload(v)
		if err != nil {
			return err
		}
		writer.Set(k, payload)
	}
	return nil
}

// stringMapCarrier adapts a map[string]string to the OTel TextMapCarrier
// interface for Inject/Extract.
type stringMapCarrier struct{ m map[string]string }

func (c stringMapCarrier) Get(key string) string { return c.m[key] }
func (c stringMapCarrier) Set(key, value string) { c.m[key] = value }
func (c stringMapCarrier) Keys() []string {
	out := make([]string, 0, len(c.m))
	for k := range c.m {
		out = append(out, k)
	}
	return out
}

// headerMap is a minimal workflow.HeaderReader/Writer over a plain map,
// for tests of both directions of the bridge.
type headerMap map[string]*common.Payload

func (h headerMap) Set(k string, p *common.Payload) { h[k] = p }
func (h headerMap) Get(k string) (*common.Payload, bool) {
	p, ok := h[k]
	return p, ok
}
func (h headerMap) ForEachKey(fn func(string, *common.Payload) error) error {
	for k, v := range h {
		if err := fn(k, v); err != nil {
			return err
		}
	}
	return nil
}
