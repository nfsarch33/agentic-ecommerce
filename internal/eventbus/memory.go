package eventbus

import (
	"context"
	"sync"
)

type subscription struct {
	group   string
	handler Handler
}

type InMemoryBus struct {
	mu          sync.RWMutex
	subs        map[EventType][]subscription
	closed      bool
	delivered   []Event
	groupOffset map[string]map[EventType]int
}

func NewInMemoryBus() *InMemoryBus {
	return &InMemoryBus{
		subs:        make(map[EventType][]subscription),
		groupOffset: make(map[string]map[EventType]int),
	}
}

func (b *InMemoryBus) Publish(ctx context.Context, event Event) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrBusClosed
	}

	b.delivered = append(b.delivered, event)

	delivered := make(map[string]bool)
	for _, sub := range b.subs[event.Type] {
		if delivered[sub.group] {
			continue
		}
		delivered[sub.group] = true
		_ = sub.handler(ctx, event)
	}
	return nil
}

func (b *InMemoryBus) Subscribe(_ context.Context, eventTypes []EventType, group string, handler Handler) error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.closed {
		return ErrBusClosed
	}

	sub := subscription{group: group, handler: handler}
	for _, et := range eventTypes {
		b.subs[et] = append(b.subs[et], sub)
	}
	return nil
}

func (b *InMemoryBus) Close() error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.closed = true
	return nil
}

func (b *InMemoryBus) Ping(_ context.Context) error {
	b.mu.RLock()
	defer b.mu.RUnlock()
	if b.closed {
		return ErrBusClosed
	}
	return nil
}

func (b *InMemoryBus) Delivered() []Event {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]Event, len(b.delivered))
	copy(out, b.delivered)
	return out
}
