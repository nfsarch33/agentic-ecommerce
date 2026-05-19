package queue

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
)

var (
	ErrJobNotFound    = errors.New("job not found")
	ErrQueueEmpty     = errors.New("queue empty")
	ErrMaxRetries     = errors.New("max retries exceeded")
)

type JobID string

type Job struct {
	ID       JobID
	Queue    string
	Payload  []byte
	Attempts int
}

type Manager struct {
	mu       sync.Mutex
	queues   map[string][]Job
	jobs     map[JobID]Job
	dead     map[JobID]Job
	seq      atomic.Uint64
	waiters  map[string][]chan struct{}
}

func NewManager() *Manager {
	return &Manager{
		queues:  make(map[string][]Job),
		jobs:    make(map[JobID]Job),
		dead:    make(map[JobID]Job),
		waiters: make(map[string][]chan struct{}),
	}
}

func (m *Manager) Enqueue(_ context.Context, queue string, payload []byte) (JobID, error) {
	m.mu.Lock()
	id := JobID(fmt.Sprintf("job-%d", m.seq.Add(1)))
	j := Job{ID: id, Queue: queue, Payload: payload, Attempts: 0}
	m.queues[queue] = append(m.queues[queue], j)
	m.jobs[id] = j
	waiters := m.waiters[queue]
	m.waiters[queue] = nil
	m.mu.Unlock()

	for _, ch := range waiters {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
	return id, nil
}

func (m *Manager) Dequeue(ctx context.Context, queue string) (Job, error) {
	for {
		m.mu.Lock()
		if len(m.queues[queue]) > 0 {
			j := m.queues[queue][0]
			m.queues[queue] = m.queues[queue][1:]
			m.mu.Unlock()
			return j, nil
		}
		ch := make(chan struct{}, 1)
		m.waiters[queue] = append(m.waiters[queue], ch)
		m.mu.Unlock()

		select {
		case <-ctx.Done():
			return Job{}, ctx.Err()
		case <-ch:
		}
	}
}

func (m *Manager) Ack(_ context.Context, jobID JobID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.jobs[jobID]; !ok {
		return ErrJobNotFound
	}
	delete(m.jobs, jobID)
	return nil
}

func (m *Manager) Retry(_ context.Context, jobID JobID) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	j.Attempts++
	m.jobs[jobID] = j
	m.queues[j.Queue] = append(m.queues[j.Queue], j)
	return nil
}

func (m *Manager) DeadLetter(_ context.Context, jobID JobID, _ string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	j, ok := m.jobs[jobID]
	if !ok {
		return ErrJobNotFound
	}
	delete(m.jobs, jobID)
	m.dead[jobID] = j
	return nil
}

func (m *Manager) IsDeadLettered(jobID JobID) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.dead[jobID]
	return ok
}
