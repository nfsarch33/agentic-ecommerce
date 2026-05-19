package reqtracing

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"sync"
	"time"
)

// TraceID is a unique identifier for a distributed trace.
type TraceID string

// SpanID is a unique identifier for a span within a trace.
type SpanID string

// NewTraceID generates a cryptographically random TraceID.
func NewTraceID() TraceID {
	return TraceID(randomHex(16))
}

// NewSpanID generates a cryptographically random SpanID.
func NewSpanID() SpanID {
	return SpanID(randomHex(8))
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("reqtracing: crypto/rand read failed: " + err.Error())
	}
	return hex.EncodeToString(b)
}

// Span represents a single unit of work within a trace.
type Span struct {
	TraceID      TraceID
	SpanID       SpanID
	ParentSpanID TraceID
	Name         string
	StartedAt    time.Time
	EndedAt      time.Time
	Tags         map[string]string
}

// Recorder is a thread-safe span store.
type Recorder struct {
	mu    sync.RWMutex
	spans map[TraceID][]*Span
}

// NewRecorder returns an initialised Recorder.
func NewRecorder() *Recorder {
	return &Recorder{spans: make(map[TraceID][]*Span)}
}

// Start creates and records a new Span for the given trace.
func (r *Recorder) Start(traceID TraceID, name string) *Span {
	span := &Span{
		TraceID:   traceID,
		SpanID:    SpanID(randomHex(8)),
		Name:      name,
		StartedAt: time.Now(),
		Tags:      make(map[string]string),
	}

	r.mu.Lock()
	r.spans[traceID] = append(r.spans[traceID], span)
	r.mu.Unlock()

	return span
}

// Finish marks the span as ended.
func (r *Recorder) Finish(span *Span) {
	if span == nil {
		return
	}
	span.EndedAt = time.Now()
}

// Spans returns all spans for the given trace ID.
func (r *Recorder) Spans(traceID TraceID) []Span {
	r.mu.RLock()
	defer r.mu.RUnlock()
	raw := r.spans[traceID]
	out := make([]Span, len(raw))
	for i, s := range raw {
		out[i] = *s
	}
	return out
}

// contextKey is the unexported type for context keys in this package.
type ContextKey struct{}

// WithTrace stores a TraceID in the context.
func WithTrace(ctx context.Context, traceID TraceID) context.Context {
	return context.WithValue(ctx, ContextKey{}, traceID)
}

// TraceFromContext retrieves the TraceID from the context.
func TraceFromContext(ctx context.Context) (TraceID, bool) {
	v, ok := ctx.Value(ContextKey{}).(TraceID)
	return v, ok
}
