package agent

import (
	"container/heap"
	"context"
	"errors"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var (
	ErrAgentAlreadyRegistered = errors.New("agent already registered")
	ErrAgentNotFound          = errors.New("agent not found")
	ErrRunNotFound            = errors.New("agent run not found")
	ErrRunNotCancellable      = errors.New("agent run is not cancellable")
)

type RunState string

const (
	RunQueued    RunState = "queued"
	RunRunning   RunState = "running"
	RunSucceeded RunState = "succeeded"
	RunFailed    RunState = "failed"
	RunCancelled RunState = "cancelled"
)

type EventType string

const (
	EventRunQueued    EventType = "agent_run_queued"
	EventRunStarted   EventType = "agent_run_started"
	EventRunSucceeded EventType = "agent_run_succeeded"
	EventRunFailed    EventType = "agent_run_failed"
	EventRunCancelled EventType = "agent_run_cancelled"
)

type Descriptor struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Description  string   `json:"description,omitempty"`
	Capabilities []string `json:"capabilities,omitempty"`
}

type Agent interface {
	Descriptor() Descriptor
	Run(ctx context.Context, task Task) (RunResult, error)
}

type Task struct {
	ID        string         `json:"id"`
	AgentID   string         `json:"agent_id"`
	Priority  int            `json:"priority"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type RunResult struct {
	Payload map[string]any `json:"payload"`
}

type RunError struct {
	Code   string `json:"code"`
	Detail string `json:"detail,omitempty"`
}

type Run struct {
	ID         string         `json:"id"`
	TaskID     string         `json:"task_id"`
	AgentID    string         `json:"agent_id"`
	State      RunState       `json:"state"`
	Priority   int            `json:"priority"`
	Input      map[string]any `json:"input,omitempty"`
	Result     map[string]any `json:"result,omitempty"`
	Error      RunError       `json:"error,omitempty"`
	CreatedAt  time.Time      `json:"created_at"`
	StartedAt  *time.Time     `json:"started_at,omitempty"`
	FinishedAt *time.Time     `json:"finished_at,omitempty"`
}

type AgentEvent struct {
	Type      EventType      `json:"type"`
	RunID     string         `json:"run_id"`
	TaskID    string         `json:"task_id"`
	AgentID   string         `json:"agent_id"`
	State     RunState       `json:"state"`
	Payload   map[string]any `json:"payload"`
	CreatedAt time.Time      `json:"created_at"`
}

type Store interface {
	CreateRun(context.Context, Run) error
	UpdateRun(context.Context, Run) error
	GetRun(context.Context, string) (Run, error)
	ListRunsByAgent(context.Context, string) ([]Run, error)
}

type EventSink interface {
	Emit(context.Context, AgentEvent) error
}

type Clock interface {
	Now() time.Time
}

type realClock struct{}

func (realClock) Now() time.Time {
	return time.Now().UTC()
}

type Registry struct {
	mu     sync.RWMutex
	agents map[string]Agent
}

func NewRegistry() *Registry {
	return &Registry{agents: make(map[string]Agent)}
}

func (r *Registry) Register(agent Agent) error {
	descriptor := agent.Descriptor()
	id := strings.TrimSpace(descriptor.ID)
	if id == "" {
		return errors.New("agent id is required")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.agents[id]; exists {
		return ErrAgentAlreadyRegistered
	}
	r.agents[id] = agent
	return nil
}

func (r *Registry) Get(id string) (Agent, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	agent, ok := r.agents[id]
	return agent, ok
}

func (r *Registry) List() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()
	descriptors := make([]Descriptor, 0, len(r.agents))
	for _, agent := range r.agents {
		descriptors = append(descriptors, agent.Descriptor())
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].ID < descriptors[j].ID
	})
	return descriptors
}

type SubmitRequest struct {
	AgentID  string
	Priority int
	Payload  map[string]any
}

type SchedulerOptions struct {
	MaxConcurrent int
}

type Scheduler struct {
	registry *Registry
	store    Store
	events   EventSink
	clock    Clock

	mu            sync.Mutex
	queue         taskPriorityQueue
	running       int
	maxConcurrent int
	sequence      int64
	waiters       map[string][]chan Run
	cancels       map[string]context.CancelFunc
}

func NewScheduler(registry *Registry, store Store, events EventSink, clock Clock, opts SchedulerOptions) *Scheduler {
	if clock == nil {
		clock = realClock{}
	}
	if events == nil {
		events = noopEventSink{}
	}
	maxConcurrent := opts.MaxConcurrent
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}
	s := &Scheduler{
		registry:      registry,
		store:         store,
		events:        events,
		clock:         clock,
		maxConcurrent: maxConcurrent,
		waiters:       make(map[string][]chan Run),
		cancels:       make(map[string]context.CancelFunc),
	}
	heap.Init(&s.queue)
	return s
}

func (s *Scheduler) Store() Store {
	return s.store
}

func (s *Scheduler) GetRun(ctx context.Context, runID string) (Run, error) {
	return s.store.GetRun(ctx, runID)
}

func (s *Scheduler) ListRunsByAgent(ctx context.Context, agentID string) ([]Run, error) {
	return s.store.ListRunsByAgent(ctx, agentID)
}

func (s *Scheduler) Submit(ctx context.Context, req SubmitRequest) (Run, error) {
	agentID := strings.TrimSpace(req.AgentID)
	if _, ok := s.registry.Get(agentID); !ok {
		return Run{}, ErrAgentNotFound
	}
	now := s.clock.Now()
	if req.Payload == nil {
		req.Payload = map[string]any{}
	}
	task := Task{
		ID:        uuid.NewString(),
		AgentID:   agentID,
		Priority:  req.Priority,
		Payload:   cloneMap(req.Payload),
		CreatedAt: now,
	}
	run := Run{
		ID:        uuid.NewString(),
		TaskID:    task.ID,
		AgentID:   agentID,
		State:     RunQueued,
		Priority:  req.Priority,
		Input:     cloneMap(req.Payload),
		CreatedAt: now,
	}
	if err := s.store.CreateRun(ctx, run); err != nil {
		return Run{}, err
	}
	_ = s.events.Emit(ctx, s.event(EventRunQueued, run, map[string]any{"priority": req.Priority}))

	s.mu.Lock()
	s.sequence++
	heap.Push(&s.queue, &queuedTask{runID: run.ID, task: task, priority: req.Priority, sequence: s.sequence})
	s.dispatchLocked()
	s.mu.Unlock()
	return run, nil
}

func (s *Scheduler) Cancel(ctx context.Context, runID string) error {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return err
	}
	switch run.State {
	case RunQueued:
		now := s.clock.Now()
		run.State = RunCancelled
		run.FinishedAt = &now
		run.Error = RunError{Code: "cancelled"}
		if err := s.store.UpdateRun(ctx, run); err != nil {
			return err
		}
		_ = s.events.Emit(ctx, s.event(EventRunCancelled, run, map[string]any{"cancelled": true}))
		s.notify(run)
		return nil
	case RunRunning:
		s.mu.Lock()
		cancel := s.cancels[runID]
		s.mu.Unlock()
		if cancel == nil {
			return ErrRunNotCancellable
		}
		cancel()
		return nil
	default:
		return ErrRunNotCancellable
	}
}

func (s *Scheduler) Wait(ctx context.Context, runID string) (Run, error) {
	run, err := s.store.GetRun(ctx, runID)
	if err != nil {
		return Run{}, err
	}
	if terminal(run.State) {
		return run, nil
	}
	ch := make(chan Run, 1)
	s.mu.Lock()
	s.waiters[runID] = append(s.waiters[runID], ch)
	s.mu.Unlock()
	select {
	case run := <-ch:
		return run, nil
	case <-ctx.Done():
		return Run{}, ctx.Err()
	}
}

func (s *Scheduler) dispatchLocked() {
	for s.running < s.maxConcurrent && s.queue.Len() > 0 {
		item := heap.Pop(&s.queue).(*queuedTask)
		run, err := s.store.GetRun(context.Background(), item.runID)
		if err != nil || run.State != RunQueued {
			continue
		}
		agent, ok := s.registry.Get(item.task.AgentID)
		if !ok {
			continue
		}
		s.running++
		ctx, cancel := context.WithCancel(context.Background())
		s.cancels[item.runID] = cancel
		go s.execute(ctx, agent, item.task, run)
	}
}

func (s *Scheduler) execute(ctx context.Context, agent Agent, task Task, run Run) {
	now := s.clock.Now()
	run.State = RunRunning
	run.StartedAt = &now
	_ = s.store.UpdateRun(ctx, run)
	_ = s.events.Emit(ctx, s.event(EventRunStarted, run, map[string]any{"priority": task.Priority}))

	result, err := agent.Run(ctx, task)
	finished := s.clock.Now()
	if errors.Is(err, context.Canceled) {
		run.State = RunCancelled
		run.Error = RunError{Code: "cancelled"}
		_ = s.events.Emit(context.Background(), s.event(EventRunCancelled, run, map[string]any{"cancelled": true}))
	} else if err != nil {
		run.State = RunFailed
		run.Error = RunError{Code: "agent_failed", Detail: err.Error()}
		_ = s.events.Emit(context.Background(), s.event(EventRunFailed, run, map[string]any{"error_code": run.Error.Code}))
	} else {
		run.State = RunSucceeded
		run.Result = cloneMap(result.Payload)
		_ = s.events.Emit(context.Background(), s.event(EventRunSucceeded, run, map[string]any{"result_keys": sortedKeys(run.Result)}))
	}
	run.FinishedAt = &finished
	_ = s.store.UpdateRun(context.Background(), run)

	s.mu.Lock()
	delete(s.cancels, run.ID)
	s.running--
	s.dispatchLocked()
	s.mu.Unlock()
	s.notify(run)
}

func (s *Scheduler) event(eventType EventType, run Run, payload map[string]any) AgentEvent {
	if payload == nil {
		payload = map[string]any{}
	}
	return AgentEvent{
		Type:      eventType,
		RunID:     run.ID,
		TaskID:    run.TaskID,
		AgentID:   run.AgentID,
		State:     run.State,
		Payload:   payload,
		CreatedAt: s.clock.Now(),
	}
}

func (s *Scheduler) notify(run Run) {
	s.mu.Lock()
	waiters := s.waiters[run.ID]
	delete(s.waiters, run.ID)
	s.mu.Unlock()
	for _, ch := range waiters {
		ch <- run
		close(ch)
	}
}

type InMemoryStore struct {
	mu   sync.RWMutex
	runs map[string]Run
}

func NewInMemoryStore() *InMemoryStore {
	return &InMemoryStore{runs: make(map[string]Run)}
}

func (s *InMemoryStore) CreateRun(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *InMemoryStore) UpdateRun(_ context.Context, run Run) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.runs[run.ID]; !ok {
		return ErrRunNotFound
	}
	s.runs[run.ID] = cloneRun(run)
	return nil
}

func (s *InMemoryStore) GetRun(_ context.Context, id string) (Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	run, ok := s.runs[id]
	if !ok {
		return Run{}, ErrRunNotFound
	}
	return cloneRun(run), nil
}

func (s *InMemoryStore) ListRunsByAgent(_ context.Context, agentID string) ([]Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	runs := make([]Run, 0)
	for _, run := range s.runs {
		if run.AgentID == agentID {
			runs = append(runs, cloneRun(run))
		}
	}
	sort.Slice(runs, func(i, j int) bool {
		return runs[i].CreatedAt.After(runs[j].CreatedAt)
	})
	return runs, nil
}

type EventRecorder struct {
	mu     sync.RWMutex
	events []AgentEvent
}

func NewEventRecorder() *EventRecorder {
	return &EventRecorder{}
}

func (r *EventRecorder) Emit(_ context.Context, event AgentEvent) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if event.Payload == nil {
		event.Payload = map[string]any{}
	}
	r.events = append(r.events, event)
	return nil
}

func (r *EventRecorder) Events() []AgentEvent {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]AgentEvent, len(r.events))
	copy(out, r.events)
	return out
}

type noopEventSink struct{}

func (noopEventSink) Emit(context.Context, AgentEvent) error {
	return nil
}

type queuedTask struct {
	runID    string
	task     Task
	priority int
	sequence int64
	index    int
}

type taskPriorityQueue []*queuedTask

func (q taskPriorityQueue) Len() int {
	return len(q)
}

func (q taskPriorityQueue) Less(i, j int) bool {
	if q[i].priority == q[j].priority {
		return q[i].sequence < q[j].sequence
	}
	return q[i].priority > q[j].priority
}

func (q taskPriorityQueue) Swap(i, j int) {
	q[i], q[j] = q[j], q[i]
	q[i].index = i
	q[j].index = j
}

func (q *taskPriorityQueue) Push(x any) {
	item := x.(*queuedTask)
	item.index = len(*q)
	*q = append(*q, item)
}

func (q *taskPriorityQueue) Pop() any {
	old := *q
	n := len(old)
	item := old[n-1]
	old[n-1] = nil
	item.index = -1
	*q = old[0 : n-1]
	return item
}

func terminal(state RunState) bool {
	switch state {
	case RunSucceeded, RunFailed, RunCancelled:
		return true
	default:
		return false
	}
}

func cloneRun(run Run) Run {
	run.Input = cloneMap(run.Input)
	run.Result = cloneMap(run.Result)
	return run
}

func cloneMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		out[key] = value
	}
	return out
}

func sortedKeys(values map[string]any) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
