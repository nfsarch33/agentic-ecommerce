package reqtracing

import (
	"context"
	"sync"
	"testing"
)

func TestNewIDs_Unique(t *testing.T) {
	t.Parallel()

	seen := make(map[TraceID]bool)
	for i := 0; i < 100; i++ {
		id := NewTraceID()
		if seen[id] {
			t.Fatalf("duplicate TraceID: %s", id)
		}
		seen[id] = true
	}

	seenSpan := make(map[SpanID]bool)
	for i := 0; i < 100; i++ {
		id := NewSpanID()
		if seenSpan[id] {
			t.Fatalf("duplicate SpanID: %s", id)
		}
		seenSpan[id] = true
	}
}

func TestRecorder_StartFinish(t *testing.T) {
	t.Parallel()

	rec := NewRecorder()
	tid := NewTraceID()
	span := rec.Start(tid, "op.do")
	if span == nil {
		t.Fatal("Start returned nil")
	}
	if span.Name != "op.do" {
		t.Errorf("Name = %q, want op.do", span.Name)
	}
	if span.StartedAt.IsZero() {
		t.Error("StartedAt is zero")
	}

	rec.Finish(span)
	if span.EndedAt.IsZero() {
		t.Error("EndedAt is zero after Finish")
	}
}

func TestRecorder_SpansByTraceID(t *testing.T) {
	t.Parallel()

	rec := NewRecorder()
	tid1 := NewTraceID()
	tid2 := NewTraceID()

	rec.Start(tid1, "a")
	rec.Start(tid1, "b")
	rec.Start(tid2, "c")

	spans1 := rec.Spans(tid1)
	if len(spans1) != 2 {
		t.Errorf("spans for tid1 = %d, want 2", len(spans1))
	}
	spans2 := rec.Spans(tid2)
	if len(spans2) != 1 {
		t.Errorf("spans for tid2 = %d, want 1", len(spans2))
	}
}

func TestContextPropagation(t *testing.T) {
	t.Parallel()

	tid := NewTraceID()
	ctx := WithTrace(context.Background(), tid)

	got, ok := TraceFromContext(ctx)
	if !ok {
		t.Fatal("TraceFromContext returned false")
	}
	if got != tid {
		t.Errorf("TraceID from context = %q, want %q", got, tid)
	}
}

func TestContextPropagation_Missing(t *testing.T) {
	t.Parallel()

	_, ok := TraceFromContext(context.Background())
	if ok {
		t.Error("expected false for context without trace")
	}
}

func TestRecorder_ConcurrentRecording(t *testing.T) {
	t.Parallel()

	rec := NewRecorder()
	var wg sync.WaitGroup
	tid := NewTraceID()

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			span := rec.Start(tid, "concurrent")
			rec.Finish(span)
		}()
	}
	wg.Wait()

	spans := rec.Spans(tid)
	if len(spans) != 50 {
		t.Errorf("spans = %d, want 50", len(spans))
	}
}
