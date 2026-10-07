package otelprovider

import (
	"context"

	attribute "go.opentelemetry.io/otel/attribute"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
)

// AllowListProcessor wraps a SpanProcessor and strips every span
// attribute (and every span event's attributes) that is not on
// AllowedKeys BEFORE the wrapped processor sees it. This repository is
// public: the allow-list is the guarantee that no host, prompt or PII
// rides a span into an exported trace, whatever future instrumentation
// attaches upstream of it.
type AllowListProcessor struct {
	next tracesdk.SpanProcessor
}

var _ tracesdk.SpanProcessor = (*AllowListProcessor)(nil)

func (p *AllowListProcessor) OnStart(parent context.Context, s tracesdk.ReadWriteSpan) {
	// Attributes set at start flow through the wrapped processor's OnEnd
	// snapshot; nothing to do here.
}

func (p *AllowListProcessor) OnEnd(s tracesdk.ReadOnlySpan) {
	p.next.OnEnd(&filteredSpan{ReadOnlySpan: s})
}

func (p *AllowListProcessor) Shutdown(ctx context.Context) error { return p.next.Shutdown(ctx) }

func (p *AllowListProcessor) ForceFlush(ctx context.Context) error { return p.next.ForceFlush(ctx) }

// filteredSpan presents a ReadOnlySpan with allow-listed attributes and
// events; everything else delegates to the wrapped span.
type filteredSpan struct {
	tracesdk.ReadOnlySpan
}

func (f *filteredSpan) Attributes() []attribute.KeyValue {
	return filterAttrs(f.ReadOnlySpan.Attributes())
}

// Status keeps the code and DROPS the description: otelhttp records
// transport errors as SetStatus(codes.Error, err.Error()), and a
// url.Error stringifies the full request URL — which carries query
// credentials on store endpoints. The code is the signal; the text is
// the leak.
func (f *filteredSpan) Status() tracesdk.Status {
	st := f.ReadOnlySpan.Status()
	if st.Description != "" {
		st.Description = "error"
	}
	return st
}

// Links carries attributes through the same allow-list.
func (f *filteredSpan) Links() []tracesdk.Link {
	ls := f.ReadOnlySpan.Links()
	out := make([]tracesdk.Link, 0, len(ls))
	for _, l := range ls {
		l.Attributes = filterAttrs(l.Attributes)
		out = append(out, l)
	}
	return out
}

func (f *filteredSpan) Events() []tracesdk.Event {
	evs := f.ReadOnlySpan.Events()
	out := make([]tracesdk.Event, 0, len(evs))
	for _, ev := range evs {
		ev.Attributes = filterAttrs(ev.Attributes)
		out = append(out, ev)
	}
	return out
}
