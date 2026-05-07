package agent

import (
	"context"
	"errors"
	"reflect"
	"sync"
	"testing"
	"time"
)

type fixedClock struct {
	now time.Time
}

func (c fixedClock) Now() time.Time {
	return c.now
}

type stubAgent struct {
	id   string
	name string
	run  func(context.Context, Task) (RunResult, error)
}

func (a stubAgent) Descriptor() Descriptor {
	return Descriptor{ID: a.id, Name: a.name}
}

func (a stubAgent) Run(ctx context.Context, task Task) (RunResult, error) {
	if a.run == nil {
		return RunResult{Payload: map[string]any{"ok": true}}, nil
	}
	return a.run(ctx, task)
}

func TestRegistryRegistersListsAndRejectsDuplicates(t *testing.T) {
	t.Parallel()

	registry := NewRegistry()
	if err := registry.Register(stubAgent{id: "pricing", name: "Pricing"}); err != nil {
		t.Fatalf("register pricing: %v", err)
	}
	if err := registry.Register(stubAgent{id: "sourcing", name: "Sourcing"}); err != nil {
		t.Fatalf("register sourcing: %v", err)
	}
	if err := registry.Register(stubAgent{id: "pricing", name: "Duplicate"}); !errors.Is(err, ErrAgentAlreadyRegistered) {
		t.Fatalf("duplicate register err = %v, want ErrAgentAlreadyRegistered", err)
	}

	descriptors := registry.List()
	got := []string{descriptors[0].ID, descriptors[1].ID}
	if want := []string{"pricing", "sourcing"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("registry list IDs = %v, want %v", got, want)
	}

	if _, ok := registry.Get("missing"); ok {
		t.Fatal("missing agent returned ok")
	}
}

func TestSchedulerRunsHigherPriorityQueuedTasksFirstWithConcurrencyLimit(t *testing.T) {
	t.Parallel()

	firstStarted := make(chan struct{})
	releaseFirst := make(chan struct{})
	var (
		mu    sync.Mutex
		order []string
	)
	agent := stubAgent{id: "worker", name: "Worker", run: func(ctx context.Context, task Task) (RunResult, error) {
		name, _ := task.Payload["name"].(string)
		if name == "first" {
			close(firstStarted)
			select {
			case <-releaseFirst:
			case <-ctx.Done():
				return RunResult{}, ctx.Err()
			}
		}
		mu.Lock()
		order = append(order, name)
		mu.Unlock()
		return RunResult{Payload: map[string]any{"name": name}}, nil
	}}

	scheduler := newTestScheduler(t, agent, SchedulerOptions{MaxConcurrent: 1})
	first, err := scheduler.Submit(context.Background(), SubmitRequest{AgentID: "worker", Priority: 1, Payload: map[string]any{"name": "first"}})
	if err != nil {
		t.Fatalf("submit first: %v", err)
	}
	<-firstStarted

	low, err := scheduler.Submit(context.Background(), SubmitRequest{AgentID: "worker", Priority: 1, Payload: map[string]any{"name": "low"}})
	if err != nil {
		t.Fatalf("submit low: %v", err)
	}
	high, err := scheduler.Submit(context.Background(), SubmitRequest{AgentID: "worker", Priority: 10, Payload: map[string]any{"name": "high"}})
	if err != nil {
		t.Fatalf("submit high: %v", err)
	}

	close(releaseFirst)
	for _, runID := range []string{first.ID, high.ID, low.ID} {
		if _, err := scheduler.Wait(context.Background(), runID); err != nil {
			t.Fatalf("wait %s: %v", runID, err)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	if want := []string{"first", "high", "low"}; !reflect.DeepEqual(order, want) {
		t.Fatalf("run order = %v, want %v", order, want)
	}
}

func TestSchedulerRecordsLifecycleTransitionsAndStructuredEvents(t *testing.T) {
	t.Parallel()

	events := NewEventRecorder()
	store := NewInMemoryStore()
	registry := NewRegistry()
	if err := registry.Register(stubAgent{id: "pricing", name: "Pricing", run: func(_ context.Context, task Task) (RunResult, error) {
		return RunResult{Payload: map[string]any{"sku": task.Payload["sku"]}}, nil
	}}); err != nil {
		t.Fatalf("register: %v", err)
	}
	scheduler := NewScheduler(registry, store, events, fixedClock{now: time.Date(2026, 5, 7, 4, 0, 0, 0, time.UTC)}, SchedulerOptions{MaxConcurrent: 2})

	submitted, err := scheduler.Submit(context.Background(), SubmitRequest{AgentID: "pricing", Priority: 3, Payload: map[string]any{"sku": "RB-SET"}})
	if err != nil {
		t.Fatalf("submit: %v", err)
	}
	completed, err := scheduler.Wait(context.Background(), submitted.ID)
	if err != nil {
		t.Fatalf("wait: %v", err)
	}
	if completed.State != RunSucceeded {
		t.Fatalf("state = %s, want %s", completed.State, RunSucceeded)
	}
	if completed.Result["sku"] != "RB-SET" {
		t.Fatalf("result = %#v", completed.Result)
	}

	recorded, err := store.GetRun(context.Background(), submitted.ID)
	if err != nil {
		t.Fatalf("get run: %v", err)
	}
	if recorded.StartedAt == nil || recorded.FinishedAt == nil {
		t.Fatalf("timestamps not set: %#v", recorded)
	}

	gotTypes := make([]EventType, 0)
	for _, event := range events.Events() {
		if event.Payload == nil {
			t.Fatalf("event payload is nil: %#v", event)
		}
		gotTypes = append(gotTypes, event.Type)
	}
	wantTypes := []EventType{EventRunQueued, EventRunStarted, EventRunSucceeded}
	if !reflect.DeepEqual(gotTypes, wantTypes) {
		t.Fatalf("event types = %v, want %v", gotTypes, wantTypes)
	}
}

func TestSchedulerMarksFailedAndCancelledRuns(t *testing.T) {
	t.Parallel()

	failAgent := stubAgent{id: "fail", name: "Fail", run: func(context.Context, Task) (RunResult, error) {
		return RunResult{}, errors.New("deterministic failure")
	}}
	scheduler := newTestScheduler(t, failAgent, SchedulerOptions{MaxConcurrent: 1})
	failed, err := scheduler.Submit(context.Background(), SubmitRequest{AgentID: "fail"})
	if err != nil {
		t.Fatalf("submit failed: %v", err)
	}
	completed, err := scheduler.Wait(context.Background(), failed.ID)
	if err != nil {
		t.Fatalf("wait failed: %v", err)
	}
	if completed.State != RunFailed || completed.Error.Code != "agent_failed" {
		t.Fatalf("failed run = %#v", completed)
	}

	blocked := make(chan struct{})
	release := make(chan struct{})
	cancelAgent := stubAgent{id: "cancel", name: "Cancel", run: func(ctx context.Context, _ Task) (RunResult, error) {
		close(blocked)
		select {
		case <-release:
			return RunResult{}, nil
		case <-ctx.Done():
			return RunResult{}, ctx.Err()
		}
	}}
	scheduler = newTestScheduler(t, cancelAgent, SchedulerOptions{MaxConcurrent: 1})
	run, err := scheduler.Submit(context.Background(), SubmitRequest{AgentID: "cancel"})
	if err != nil {
		t.Fatalf("submit cancel: %v", err)
	}
	<-blocked
	if err := scheduler.Cancel(context.Background(), run.ID); err != nil {
		t.Fatalf("cancel: %v", err)
	}
	completed, err = scheduler.Wait(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("wait cancel: %v", err)
	}
	close(release)
	if completed.State != RunCancelled {
		t.Fatalf("cancelled state = %s, want %s", completed.State, RunCancelled)
	}
}

func newTestScheduler(t *testing.T, a Agent, opts SchedulerOptions) *Scheduler {
	t.Helper()
	registry := NewRegistry()
	if err := registry.Register(a); err != nil {
		t.Fatalf("register: %v", err)
	}
	return NewScheduler(registry, NewInMemoryStore(), NewEventRecorder(), realClock{}, opts)
}
