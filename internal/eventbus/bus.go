package eventbus

import "context"

type Publisher interface {
	Publish(ctx context.Context, event Event) error
	Close() error
}

type Handler func(ctx context.Context, event Event) error

type Consumer interface {
	Subscribe(ctx context.Context, eventTypes []EventType, group string, handler Handler) error
	Close() error
}

type HealthChecker interface {
	Ping(ctx context.Context) error
}
