package agentrace

import "context"

// ringBuffer is a bounded FIFO backed by a buffered channel so the
// writer goroutine can drain via a non-blocking select. Push honours
// the caller's context budget; pop is non-blocking.
type ringBuffer struct {
	ch chan Event
}

func newRingBuffer(capacity int) *ringBuffer {
	if capacity <= 0 {
		capacity = 1
	}
	return &ringBuffer{ch: make(chan Event, capacity)}
}

// push enqueues ev. Returns true on success; false when ctx fires
// before a slot frees up.
func (r *ringBuffer) push(ctx context.Context, ev Event) bool {
	select {
	case r.ch <- ev:
		return true
	case <-ctx.Done():
		return false
	}
}

// pop returns the next event without blocking; ok is false when the
// ring is empty.
func (r *ringBuffer) pop() (Event, bool) {
	select {
	case ev := <-r.ch:
		return ev, true
	default:
		return Event{}, false
	}
}

func (r *ringBuffer) len() int {
	return len(r.ch)
}
