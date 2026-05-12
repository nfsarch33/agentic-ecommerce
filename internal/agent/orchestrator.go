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
	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

var (
	ErrAgentAlreadyRegistered = errors.New("agent already registered")
	ErrAgentNotFound          = errors.New("agent not found")
	ErrRunNotFound            = errors.New("agent run not found")
	ErrRunNotCancellable      = errors.New("agent run is not cancellable")
	ErrSchedulerClosed        = errors.New("agent scheduler closed")
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
	Metrics       workerpool.PoolMetrics
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
	pool          *workerpool.Pool
	closed        bool
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
	s.pool = workerpool.New(nil, workerpool.Config{
		Name:       "agent-scheduler",
		MinWorkers: maxConcurrent,
		MaxWorkers: maxConcurrent,
		QueueDepth: maxConcurrent,
		Metrics:    opts.Metrics,
	})
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
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return Run{}, ErrSchedulerClosed
	}
	s.mu.Unlock()

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
	if s.closed {
		s.mu.Unlock()
		s.cancelRun(ctx, run, "scheduler_closed")
		return run, ErrSchedulerClosed
	}
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
	if s.closed {
		return
	}
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
		if err := s.pool.Submit(ctx, func(execCtx context.Context) error {
			s.execute(execCtx, agent, item.task, run)
			return nil
		}); err != nil {
			cancel()
			delete(s.cancels, item.runID)
			s.running--
			s.failDispatch(run, err)
		}
	}
}

func (s *Scheduler) failDispatch(run Run, err error) {
	now := s.clock.Now()
	run.State = RunFailed
	run.FinishedAt = &now
	run.Error = RunError{Code: "scheduler_dispatch_failed", Detail: err.Error()}
	_ = s.store.UpdateRun(context.Background(), run)
	_ = s.events.Emit(context.Background(), s.event(EventRunFailed, run, map[string]any{"error_code": run.Error.Code}))
	s.notifyLocked(run)
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

// Close cancels queued and running work, rejects future submissions, and
// drains the scheduler worker pool. It is safe to call more than once.
func (s *Scheduler) Close(ctx context.Context) error {
	if ctx == nil {
		ctx = context.Background()
	}

	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	queued := make([]*queuedTask, 0, s.queue.Len())
	for s.queue.Len() > 0 {
		queued = append(queued, heap.Pop(&s.queue).(*queuedTask))
	}
	cancels := make([]context.CancelFunc, 0, len(s.cancels))
	for _, cancel := range s.cancels {
		cancels = append(cancels, cancel)
	}
	s.mu.Unlock()

	for _, item := range queued {
		if run, err := s.store.GetRun(ctx, item.runID); err == nil && run.State == RunQueued {
			s.cancelRun(ctx, run, "scheduler_closed")
		}
	}
	for _, cancel := range cancels {
		cancel()
	}
	return s.pool.Close(ctx)
}

func (s *Scheduler) cancelRun(ctx context.Context, run Run, code string) {
	now := s.clock.Now()
	run.State = RunCancelled
	run.FinishedAt = &now
	run.Error = RunError{Code: code}
	_ = s.store.UpdateRun(ctx, run)
	_ = s.events.Emit(ctx, s.event(EventRunCancelled, run, map[string]any{"cancelled": true}))
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
	s.notifyLocked(run)
	s.mu.Unlock()
}

func (s *Scheduler) notifyLocked(run Run) {
	waiters := s.waiters[run.ID]
	delete(s.waiters, run.ID)
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
