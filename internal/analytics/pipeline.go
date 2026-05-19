package analytics

import (
	"context"
	"sync"
	"time"
)

// AnalyticsEvent is a single trackable event.
type AnalyticsEvent struct {
	Name      string
	UserID    string
	Timestamp time.Time
	Data      map[string]any
}

// Metric aggregates event counts and values.
type Metric struct {
	Count int64
}

// FlushFunc is called when the buffer is flushed.
type FlushFunc func(events []AnalyticsEvent)

// PipelineConfig configures an EventPipeline.
type PipelineConfig struct {
	BufferSize    int
	FlushInterval time.Duration
	FlushFn       FlushFunc
}

// EventPipeline buffers analytics events and flushes them in batches.
type EventPipeline struct {
	cfg     PipelineConfig
	ch      chan AnalyticsEvent
	done    chan struct{}
	closed  chan struct{}
	once    sync.Once
}

func NewEventPipeline(cfg PipelineConfig) *EventPipeline {
	p := &EventPipeline{
		cfg:    cfg,
		ch:     make(chan AnalyticsEvent, cfg.BufferSize*4),
		done:   make(chan struct{}),
		closed: make(chan struct{}),
	}
	go p.run()
	return p
}

// Emit enqueues an event. Non-blocking; drops events if the channel is full.
func (p *EventPipeline) Emit(event AnalyticsEvent) {
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	select {
	case p.ch <- event:
	default:
	}
}

// Close flushes remaining events and stops the pipeline.
func (p *EventPipeline) Close() {
	p.CloseContext(context.Background())
}

// CloseContext flushes remaining events respecting the context deadline.
func (p *EventPipeline) CloseContext(ctx context.Context) {
	p.once.Do(func() { close(p.done) })
	select {
	case <-p.closed:
	case <-ctx.Done():
	}
}

func (p *EventPipeline) run() {
	defer close(p.closed)
	ticker := time.NewTicker(p.cfg.FlushInterval)
	defer ticker.Stop()

	buf := make([]AnalyticsEvent, 0, p.cfg.BufferSize)

	flush := func() {
		if len(buf) == 0 {
			return
		}
		toFlush := buf
		buf = make([]AnalyticsEvent, 0, p.cfg.BufferSize)
		p.cfg.FlushFn(toFlush)
	}

	for {
		select {
		case evt := <-p.ch:
			buf = append(buf, evt)
			if len(buf) >= p.cfg.BufferSize {
				flush()
			}
		case <-ticker.C:
			flush()
		case <-p.done:
			// drain channel
			for {
				select {
				case evt := <-p.ch:
					buf = append(buf, evt)
				default:
					flush()
					return
				}
			}
		}
	}
}

// Aggregate computes per-event-name metrics from a slice of events.
func Aggregate(events []AnalyticsEvent) map[string]Metric {
	out := make(map[string]Metric)
	for _, e := range events {
		m := out[e.Name]
		m.Count++
		out[e.Name] = m
	}
	return out
}
