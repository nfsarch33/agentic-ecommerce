package eventbus

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

type StreamsConfig struct {
	RedisAddr     string
	RedisDB       int
	RedisPassword string
	StreamPrefix  string
	ConsumerGroup string
	ConsumerID    string
	ClaimMinIdle  time.Duration
}

func (c StreamsConfig) streamKey(eventType EventType) string {
	prefix := c.StreamPrefix
	if prefix == "" {
		prefix = "ec:events"
	}
	return fmt.Sprintf("%s:%s", prefix, string(eventType))
}

type XAddArgs struct {
	Stream string
	Values map[string]any
}

type XReadGroupArgs struct {
	Group    string
	Consumer string
	Streams  []string
	Count    int64
	Block    time.Duration
}

type XStream struct {
	Stream   string
	Messages []XMessage
}

type XAutoClaimArgs struct {
	Stream   string
	Group    string
	Consumer string
	MinIdle  time.Duration
	Start    string
	Count    int64
}

type XMessage struct {
	ID     string
	Values map[string]any
}

// RedisStreamer is the minimal interface for Redis Streams operations.
// Implementations wrap go-redis or any RESP-compatible client.
type RedisStreamer interface {
	XAdd(ctx context.Context, args XAddArgs) error
	XGroupCreateMkStream(ctx context.Context, stream, group, start string) error
	XReadGroup(ctx context.Context, args XReadGroupArgs) ([]XStream, error)
	XAutoClaim(ctx context.Context, args XAutoClaimArgs) ([]XStream, error)
	XAck(ctx context.Context, stream, group string, ids ...string) error
	Ping(ctx context.Context) error
	Close() error
}

type StreamsPublisher struct {
	client RedisStreamer
	cfg    StreamsConfig
}

func NewStreamsPublisher(client RedisStreamer, cfg StreamsConfig) *StreamsPublisher {
	return &StreamsPublisher{client: client, cfg: cfg}
}

func (p *StreamsPublisher) Publish(ctx context.Context, event Event) error {
	if event.ID == "" {
		event.ID = uuid.NewString()
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	stream := p.cfg.streamKey(event.Type)
	return p.client.XAdd(ctx, XAddArgs{
		Stream: stream,
		Values: map[string]any{
			"id":              event.ID,
			"idempotency_key": event.ID,
			"type":            string(event.Type),
			"tenant_id":       event.TenantID,
			"source":          event.Source,
			"payload":         string(payload),
			"timestamp":       event.Timestamp.Format(time.RFC3339Nano),
		},
	})
}

func (p *StreamsPublisher) Close() error {
	return p.client.Close()
}

func (p *StreamsPublisher) Ping(ctx context.Context) error {
	return p.client.Ping(ctx)
}

type StreamsConsumer struct {
	client RedisStreamer
	cfg    StreamsConfig
	cancel context.CancelFunc
}

func NewStreamsConsumer(client RedisStreamer, cfg StreamsConfig) *StreamsConsumer {
	return &StreamsConsumer{client: client, cfg: cfg}
}

func (c *StreamsConsumer) Subscribe(ctx context.Context, eventTypes []EventType, group string, handler Handler) error {
	consumerID := c.cfg.ConsumerID
	if consumerID == "" {
		consumerID = uuid.NewString()
	}

	streams := make([]string, 0, len(eventTypes)*2)
	for _, et := range eventTypes {
		stream := c.cfg.streamKey(et)
		_ = c.client.XGroupCreateMkStream(ctx, stream, group, "0")
		streams = append(streams, stream)
	}
	for range eventTypes {
		streams = append(streams, ">")
	}

	subCtx, cancel := context.WithCancel(ctx)
	c.cancel = cancel

	go c.readLoop(subCtx, streams, group, consumerID, handler)
	return nil
}

func (c *StreamsConsumer) readLoop(ctx context.Context, streams []string, group, consumerID string, handler Handler) {
	streamCount := len(streams) / 2
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		c.reclaimPending(ctx, streams[:streamCount], group, consumerID, handler)

		result, err := c.client.XReadGroup(ctx, XReadGroupArgs{
			Group:    group,
			Consumer: consumerID,
			Streams:  streams,
			Count:    10,
			Block:    time.Second,
		})
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			time.Sleep(100 * time.Millisecond)
			continue
		}

		c.processStreams(ctx, result, group, handler)
	}
}

func (c *StreamsConsumer) reclaimPending(ctx context.Context, streams []string, group, consumerID string, handler Handler) {
	minIdle := c.cfg.ClaimMinIdle
	if minIdle <= 0 {
		minIdle = time.Minute
	}
	for _, stream := range streams {
		result, err := c.client.XAutoClaim(ctx, XAutoClaimArgs{
			Stream:   stream,
			Group:    group,
			Consumer: consumerID,
			MinIdle:  minIdle,
			Start:    "0-0",
			Count:    10,
		})
		if err == nil {
			c.processStreams(ctx, result, group, handler)
		}
	}
}

func (c *StreamsConsumer) processStreams(ctx context.Context, result []XStream, group string, handler Handler) {
	for _, stream := range result {
		for _, msg := range stream.Messages {
			event, parseErr := parseStreamMessage(msg)
			if parseErr != nil {
				continue
			}
			if err := handler(ctx, event); err == nil {
				_ = c.client.XAck(ctx, stream.Stream, group, msg.ID)
			}
		}
	}
}

func (c *StreamsConsumer) Close() error {
	if c.cancel != nil {
		c.cancel()
	}
	return c.client.Close()
}

func (c *StreamsConsumer) Ping(ctx context.Context) error {
	return c.client.Ping(ctx)
}

func parseStreamMessage(msg XMessage) (Event, error) {
	payloadStr, ok := msg.Values["payload"].(string)
	if !ok {
		return Event{}, fmt.Errorf("missing payload field")
	}
	var event Event
	if err := json.Unmarshal([]byte(payloadStr), &event); err != nil {
		return Event{}, err
	}
	return event, nil
}
