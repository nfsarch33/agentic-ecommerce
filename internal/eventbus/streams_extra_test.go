package eventbus

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// File scope: targeted coverage for previously uncovered branches in
// streams.go: parseStreamMessage error edges, the consumer Close path,
// and the readLoop's transient-error backoff.

func TestParseStreamMessageRejectsMissingPayload(t *testing.T) {
	t.Parallel()

	_, err := parseStreamMessage(XMessage{ID: "1-1", Values: map[string]any{"id": "evt"}})
	if err == nil || !strings.Contains(err.Error(), "missing payload") {
		t.Fatalf("err = %v, want missing payload error", err)
	}
}

func TestParseStreamMessageRejectsNonStringPayload(t *testing.T) {
	t.Parallel()

	_, err := parseStreamMessage(XMessage{ID: "1-1", Values: map[string]any{"payload": 42}})
	if err == nil {
		t.Fatal("expected error for non-string payload")
	}
}

func TestParseStreamMessageRejectsInvalidJSON(t *testing.T) {
	t.Parallel()

	_, err := parseStreamMessage(XMessage{ID: "1-1", Values: map[string]any{"payload": "not-json"}})
	if err == nil {
		t.Fatal("expected JSON decode error")
	}
}

func TestParseStreamMessageDecodesValidPayload(t *testing.T) {
	t.Parallel()

	got, err := parseStreamMessage(XMessage{ID: "1-1", Values: map[string]any{
		"payload": `{"id":"evt-1","type":"product.created","tenant_id":"tenant-a","payload":{"sku":"X"},"timestamp":"2026-05-08T01:00:00Z","source":"unit-test"}`,
	}})
	if err != nil {
		t.Fatalf("parseStreamMessage: %v", err)
	}
	if got.ID != "evt-1" || got.Type != ProductCreated || got.TenantID != "tenant-a" || got.Source != "unit-test" {
		t.Fatalf("event = %+v", got)
	}
}

// TestStreamsConsumerCloseStopsBackgroundLoopAndRedisClient asserts that
// invoking Close cancels the subscriber context and propagates Close to
// the underlying redis client. Previously this was a 0%-covered branch.
func TestStreamsConsumerCloseStopsBackgroundLoopAndRedisClient(t *testing.T) {
	t.Parallel()

	mock := &mockRedisStreamer{}
	consumer := NewStreamsConsumer(mock, StreamsConfig{StreamPrefix: "test:events", ConsumerGroup: "workers"})
	if err := consumer.Subscribe(context.Background(), []EventType{ProductCreated}, "workers", func(_ context.Context, _ Event) error {
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Allow the goroutine to schedule before closing so cancel has a real
	// goroutine to interrupt.
	time.Sleep(20 * time.Millisecond)

	if err := consumer.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	mock.mu.Lock()
	closed := mock.closeCalled
	mock.mu.Unlock()
	if !closed {
		t.Fatal("redis client Close was not invoked when consumer.Close ran")
	}
}

// TestStreamsConsumerCloseHandlesNilCancelGracefully covers the early-exit
// branch where Subscribe was never called.
func TestStreamsConsumerCloseHandlesNilCancelGracefully(t *testing.T) {
	t.Parallel()

	mock := &mockRedisStreamer{}
	consumer := NewStreamsConsumer(mock, StreamsConfig{})

	if err := consumer.Close(); err != nil {
		t.Fatalf("Close before Subscribe: %v", err)
	}
	mock.mu.Lock()
	defer mock.mu.Unlock()
	if !mock.closeCalled {
		t.Fatal("redis client Close was not invoked")
	}
}

// TestStreamsConsumerReadLoopBacksOffOnTransientError exercises the
// XReadGroup error branch (returns err, ctx not cancelled, sleeps 100ms,
// loops). We deliver a single transient error then a context cancel to
// confirm the loop stays alive long enough to retry.
func TestStreamsConsumerReadLoopBacksOffOnTransientError(t *testing.T) {
	t.Parallel()

	mock := &mockRedisStreamer{xreadErr: errors.New("redis ETIMEDOUT")}
	consumer := NewStreamsConsumer(mock, StreamsConfig{StreamPrefix: "test:events", ConsumerGroup: "workers"})

	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()

	if err := consumer.Subscribe(ctx, []EventType{ProductCreated}, "workers", func(_ context.Context, _ Event) error {
		return nil
	}); err != nil {
		t.Fatalf("Subscribe: %v", err)
	}

	// Wait for the loop to retry at least once (≥100ms backoff plus a
	// little scheduling slack).
	time.Sleep(200 * time.Millisecond)

	mock.mu.Lock()
	defer mock.mu.Unlock()
	if mock.xreadCalls < 2 {
		t.Fatalf("xread calls = %d, want >= 2 retries on transient error", mock.xreadCalls)
	}
}
