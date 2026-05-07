package eventbus

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

type mockRedisStreamer struct {
	mu          sync.Mutex
	xaddCalls   []XAddArgs
	xgroupCalls []string
	xreadCalls  int
	xackCalls   []mockXAckCall
	xreadResult []XStream
	xreadErr    error
	pingCalled  bool
	pingErr     error
	closeCalled bool
}

type mockXAckCall struct {
	Stream string
	Group  string
	IDs    []string
}

func (m *mockRedisStreamer) XAdd(_ context.Context, args XAddArgs) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xaddCalls = append(m.xaddCalls, args)
	return nil
}

func (m *mockRedisStreamer) XGroupCreateMkStream(_ context.Context, stream, group, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xgroupCalls = append(m.xgroupCalls, stream+":"+group)
	return nil
}

func (m *mockRedisStreamer) XReadGroup(_ context.Context, _ XReadGroupArgs) ([]XStream, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xreadCalls++
	if m.xreadErr != nil {
		return nil, m.xreadErr
	}
	return m.xreadResult, nil
}

func (m *mockRedisStreamer) XAck(_ context.Context, stream, group string, ids ...string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.xackCalls = append(m.xackCalls, mockXAckCall{Stream: stream, Group: group, IDs: ids})
	return nil
}

func (m *mockRedisStreamer) Ping(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.pingCalled = true
	return m.pingErr
}

func (m *mockRedisStreamer) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closeCalled = true
	return nil
}

func (m *mockRedisStreamer) ackCount() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.xackCalls)
}

func (m *mockRedisStreamer) ackCallsCopy() []mockXAckCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]mockXAckCall, len(m.xackCalls))
	copy(out, m.xackCalls)
	return out
}

func TestStreamsPublisher_Publish(t *testing.T) {
	mock := &mockRedisStreamer{}
	cfg := StreamsConfig{StreamPrefix: "test:events", ConsumerGroup: "cg1"}
	pub := NewStreamsPublisher(mock, cfg)

	event := Event{
		ID:        "evt-100",
		Type:      ProductCreated,
		TenantID:  "tenant-x",
		Payload:   map[string]any{"sku": "ABC"},
		Timestamp: time.Date(2026, 5, 7, 12, 0, 0, 0, time.UTC),
		Source:    "unit-test",
	}

	if err := pub.Publish(context.Background(), event); err != nil {
		t.Fatalf("publish: %v", err)
	}

	if len(mock.xaddCalls) != 1 {
		t.Fatalf("xadd calls = %d, want 1", len(mock.xaddCalls))
	}
	call := mock.xaddCalls[0]
	if call.Stream != "test:events:product.created" {
		t.Errorf("stream = %q, want %q", call.Stream, "test:events:product.created")
	}
	if call.Values["tenant_id"] != "tenant-x" {
		t.Errorf("tenant_id = %v, want tenant-x", call.Values["tenant_id"])
	}
	if call.Values["source"] != "unit-test" {
		t.Errorf("source = %v, want unit-test", call.Values["source"])
	}
}

func TestStreamsPublisher_Ping(t *testing.T) {
	mock := &mockRedisStreamer{}
	pub := NewStreamsPublisher(mock, StreamsConfig{})

	if err := pub.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
	if !mock.pingCalled {
		t.Error("ping not called on redis client")
	}
}

func TestStreamsPublisher_PingError(t *testing.T) {
	mock := &mockRedisStreamer{pingErr: errors.New("connection refused")}
	pub := NewStreamsPublisher(mock, StreamsConfig{})

	if err := pub.Ping(context.Background()); err == nil {
		t.Error("expected ping error, got nil")
	}
}

func TestStreamsPublisher_Close(t *testing.T) {
	mock := &mockRedisStreamer{}
	pub := NewStreamsPublisher(mock, StreamsConfig{})
	_ = pub.Close()
	if !mock.closeCalled {
		t.Error("close not called on redis client")
	}
}

func TestStreamsConsumer_SubscribeCreatesGroup(t *testing.T) {
	mock := &mockRedisStreamer{xreadErr: context.Canceled}
	cfg := StreamsConfig{StreamPrefix: "test:events", ConsumerGroup: "workers", ConsumerID: "w1"}
	consumer := NewStreamsConsumer(mock, cfg)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_ = consumer.Subscribe(ctx, []EventType{ProductCreated, OrderPlaced}, "workers", func(_ context.Context, _ Event) error { return nil })
	time.Sleep(50 * time.Millisecond)

	mock.mu.Lock()
	groups := make([]string, len(mock.xgroupCalls))
	copy(groups, mock.xgroupCalls)
	mock.mu.Unlock()

	if len(groups) != 2 {
		t.Fatalf("xgroup calls = %d, want 2", len(groups))
	}
	if groups[0] != "test:events:product.created:workers" {
		t.Errorf("xgroup[0] = %q", groups[0])
	}
	if groups[1] != "test:events:order.placed:workers" {
		t.Errorf("xgroup[1] = %q", groups[1])
	}
}

func TestStreamsConsumer_HandlerSuccessAcks(t *testing.T) {
	mock := &mockRedisStreamer{
		xreadResult: []XStream{
			{
				Stream: "test:events:product.created",
				Messages: []XMessage{
					{ID: "1-1", Values: map[string]any{
						"payload": `{"id":"e1","type":"product.created","tenant_id":"t1","payload":{},"timestamp":"2026-05-07T12:00:00Z","source":"test"}`,
					}},
				},
			},
		},
	}
	cfg := StreamsConfig{StreamPrefix: "test:events", ConsumerGroup: "workers", ConsumerID: "w1"}
	consumer := NewStreamsConsumer(mock, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = consumer.Subscribe(ctx, []EventType{ProductCreated}, "workers", func(_ context.Context, _ Event) error {
		return nil
	})

	time.Sleep(250 * time.Millisecond)

	if mock.ackCount() == 0 {
		t.Error("expected xack call after successful handler")
	}
}

func TestStreamsConsumer_HandlerFailureNoAck(t *testing.T) {
	mock := &mockRedisStreamer{
		xreadResult: []XStream{
			{
				Stream: "test:events:product.created",
				Messages: []XMessage{
					{ID: "1-1", Values: map[string]any{
						"payload": `{"id":"e1","type":"product.created","tenant_id":"t1","payload":{},"timestamp":"2026-05-07T12:00:00Z","source":"test"}`,
					}},
				},
			},
		},
	}
	cfg := StreamsConfig{StreamPrefix: "test:events", ConsumerGroup: "workers", ConsumerID: "w1"}
	consumer := NewStreamsConsumer(mock, cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	_ = consumer.Subscribe(ctx, []EventType{ProductCreated}, "workers", func(_ context.Context, _ Event) error {
		return errors.New("processing failed")
	})

	time.Sleep(250 * time.Millisecond)

	for _, call := range mock.ackCallsCopy() {
		for _, id := range call.IDs {
			if id == "1-1" {
				t.Error("should not ack message when handler fails")
			}
		}
	}
}

func TestStreamsConsumer_Ping(t *testing.T) {
	mock := &mockRedisStreamer{}
	consumer := NewStreamsConsumer(mock, StreamsConfig{})
	if err := consumer.Ping(context.Background()); err != nil {
		t.Errorf("ping: %v", err)
	}
	if !mock.pingCalled {
		t.Error("ping not called")
	}
}

func TestStreamConfig_StreamKey(t *testing.T) {
	cfg := StreamsConfig{StreamPrefix: "myapp:events"}
	got := cfg.streamKey(ProductCreated)
	want := "myapp:events:product.created"
	if got != want {
		t.Errorf("streamKey = %q, want %q", got, want)
	}
}

func TestStreamConfig_StreamKeyDefault(t *testing.T) {
	cfg := StreamsConfig{}
	got := cfg.streamKey(OrderPlaced)
	want := "ec:events:order.placed"
	if got != want {
		t.Errorf("streamKey default = %q, want %q", got, want)
	}
}
