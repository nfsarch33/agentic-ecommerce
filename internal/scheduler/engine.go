package scheduler

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

var (
	ErrTaskNotFound      = errors.New("task not found")
	ErrInvalidCron       = errors.New("invalid cron expression")
	ErrDuplicateHandler  = errors.New("handler already registered")
	ErrMaxRetriesExceeded = errors.New("max retries exceeded")
)

type TaskFunc func(ctx interface{}) error

type TaskResult struct {
	TaskID   TaskID
	Success  bool
	Attempts int
	Err      error
}

type TaskID string

type scheduledTask struct {
	id       TaskID
	name     string
	cron     string
	handler  TaskFunc
	cancelled bool
}

type Engine struct {
	mu       sync.RWMutex
	handlers map[string]TaskFunc
	tasks    map[TaskID]*scheduledTask
	seq      int
}

func NewEngine() *Engine {
	return &Engine{
		handlers: make(map[string]TaskFunc),
		tasks:    make(map[TaskID]*scheduledTask),
	}
}

func (e *Engine) Register(name string, handler TaskFunc) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.handlers[name]; ok {
		return ErrDuplicateHandler
	}
	e.handlers[name] = handler
	return nil
}

func (e *Engine) Schedule(name, cron string) (TaskID, error) {
	if !validCron(cron) {
		return "", ErrInvalidCron
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	handler, ok := e.handlers[name]
	if !ok {
		return "", ErrTaskNotFound
	}
	e.seq++
	id := TaskID(fmt.Sprintf("TASK-%04d", e.seq))
	e.tasks[id] = &scheduledTask{id: id, name: name, cron: cron, handler: handler}
	return id, nil
}

func (e *Engine) Execute(taskID TaskID) (TaskResult, error) {
	e.mu.RLock()
	t, ok := e.tasks[taskID]
	e.mu.RUnlock()
	if !ok {
		return TaskResult{}, ErrTaskNotFound
	}
	err := t.handler(nil)
	return TaskResult{TaskID: taskID, Success: err == nil, Attempts: 1, Err: err}, nil
}

func (e *Engine) Retry(taskID TaskID, maxRetries int) error {
	e.mu.RLock()
	t, ok := e.tasks[taskID]
	e.mu.RUnlock()
	if !ok {
		return ErrTaskNotFound
	}
	delay := time.Millisecond
	for i := 0; i < maxRetries; i++ {
		if err := t.handler(nil); err == nil {
			return nil
		}
		time.Sleep(delay)
		delay *= 2
	}
	return ErrMaxRetriesExceeded
}

func (e *Engine) Cancel(taskID TaskID) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	t, ok := e.tasks[taskID]
	if !ok {
		return ErrTaskNotFound
	}
	t.cancelled = true
	return nil
}

// validCron does minimal validation: expects 5 space-separated fields.
func validCron(cron string) bool {
	parts := strings.Fields(cron)
	return len(parts) == 5
}
