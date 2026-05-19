package analytics_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/analytics"
)

func TestPipeline_BuffersEvents(t *testing.T) {
	t.Parallel()
	flushed := make([]analytics.AnalyticsEvent, 0)
	var mu sync.Mutex
	flushFn := func(events []analytics.AnalyticsEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushed = append(flushed, events...)
	}

	p := analytics.NewEventPipeline(analytics.PipelineConfig{
		BufferSize:    5,
		FlushInterval: time.Minute,
		FlushFn:       flushFn,
	})
	defer p.Close()

	for i := 0; i < 3; i++ {
		p.Emit(analytics.AnalyticsEvent{Name: "page_view"})
	}

	// not yet flushed -- under threshold
	time.Sleep(10 * time.Millisecond)
	mu.Lock()
	count := len(flushed)
	mu.Unlock()
	if count != 0 {
		t.Fatalf("should not have flushed yet, got %d", count)
	}
}

func TestPipeline_FlushOnThreshold(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	flushed := 0
	flushFn := func(events []analytics.AnalyticsEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushed += len(events)
	}

	p := analytics.NewEventPipeline(analytics.PipelineConfig{
		BufferSize:    3,
		FlushInterval: time.Minute,
		FlushFn:       flushFn,
	})
	defer p.Close()

	for i := 0; i < 3; i++ {
		p.Emit(analytics.AnalyticsEvent{Name: "click"})
	}
	time.Sleep(20 * time.Millisecond)

	mu.Lock()
	n := flushed
	mu.Unlock()
	if n != 3 {
		t.Fatalf("expected 3 flushed events, got %d", n)
	}
}

func TestPipeline_FlushOnInterval(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	flushed := 0
	flushFn := func(events []analytics.AnalyticsEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushed += len(events)
	}

	p := analytics.NewEventPipeline(analytics.PipelineConfig{
		BufferSize:    100,
		FlushInterval: 30 * time.Millisecond,
		FlushFn:       flushFn,
	})
	defer p.Close()

	p.Emit(analytics.AnalyticsEvent{Name: "purchase"})
	time.Sleep(60 * time.Millisecond)

	mu.Lock()
	n := flushed
	mu.Unlock()
	if n == 0 {
		t.Fatal("expected flush on interval")
	}
}

func TestAggregate_Counts(t *testing.T) {
	t.Parallel()
	events := []analytics.AnalyticsEvent{
		{Name: "page_view"}, {Name: "page_view"}, {Name: "click"},
	}
	metrics := analytics.Aggregate(events)
	if metrics["page_view"].Count != 2 {
		t.Fatalf("expected 2 page_views, got %v", metrics["page_view"])
	}
	if metrics["click"].Count != 1 {
		t.Fatalf("expected 1 click, got %v", metrics["click"])
	}
}

func TestPipeline_ConcurrentEmit_RaceFree(t *testing.T) {
	t.Parallel()
	p := analytics.NewEventPipeline(analytics.PipelineConfig{
		BufferSize:    1000,
		FlushInterval: time.Minute,
		FlushFn:       func([]analytics.AnalyticsEvent) {},
	})
	defer p.Close()

	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			p.Emit(analytics.AnalyticsEvent{Name: "concurrent"})
		}()
	}
	wg.Wait()
}

func TestPipeline_Close_FlushesRemaining(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	flushed := 0
	flushFn := func(events []analytics.AnalyticsEvent) {
		mu.Lock()
		defer mu.Unlock()
		flushed += len(events)
	}

	p := analytics.NewEventPipeline(analytics.PipelineConfig{
		BufferSize:    100,
		FlushInterval: time.Minute,
		FlushFn:       flushFn,
	})

	p.Emit(analytics.AnalyticsEvent{Name: "final"})
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	p.CloseContext(ctx)

	mu.Lock()
	n := flushed
	mu.Unlock()
	if n == 0 {
		t.Fatal("expected Close to flush remaining events")
	}
}
