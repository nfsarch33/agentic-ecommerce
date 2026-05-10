// Package redis pipeline provides batch command execution to reduce
// round-trips. A Pipeline accumulates commands and flushes them in a
// single network call, amortising RTT across all queued operations.
//
// Design: the pipeline is intentionally simple — it wraps a slice of
// PipelineCmd and a flush function. This lets callers batch GETs,
// SETs, MULTI/EXEC blocks, and arbitrary commands without coupling
// to a specific Redis client library. The flush function is injected
// at construction time; tests wire an in-memory fake, production
// wires the go-redis Pipeliner or a raw RESP connection.
package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

// MaxPipelineSize caps the number of commands per pipeline flush.
// Prevents unbounded memory growth from a runaway batch.
const MaxPipelineSize = 1000

var (
	ErrPipelineClosed = errors.New("redis: pipeline closed")
	ErrPipelineFull   = errors.New("redis: pipeline full")
	ErrPipelineEmpty  = errors.New("redis: pipeline empty")
	ErrPartialFailure = errors.New("redis: pipeline partial failure")
)

// PipelineCmd is a single Redis command in the batch.
type PipelineCmd struct {
	Op   string
	Key  string
	Args []any
}

// PipelineResult is the result for a single command in the batch.
type PipelineResult struct {
	Value any
	Err   error
}

// FlushFunc executes a batch of commands atomically (or pipelined)
// and returns one result per command. Production wires the go-redis
// Pipeliner; tests wire an in-memory fake.
type FlushFunc func(ctx context.Context, cmds []PipelineCmd) ([]PipelineResult, error)

// Pipeline accumulates Redis commands and flushes them in a single
// network call.
type Pipeline struct {
	mu      sync.Mutex
	cmds    []PipelineCmd
	flush   FlushFunc
	closed  bool
	maxSize int
}

// NewPipeline constructs a pipeline with the given flush function.
func NewPipeline(flush FlushFunc) *Pipeline {
	return &Pipeline{
		cmds:    make([]PipelineCmd, 0, 16),
		flush:   flush,
		maxSize: MaxPipelineSize,
	}
}

// Add queues a command for the next flush.
func (p *Pipeline) Add(cmd PipelineCmd) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		return ErrPipelineClosed
	}
	if len(p.cmds) >= p.maxSize {
		return fmt.Errorf("%w: limit=%d", ErrPipelineFull, p.maxSize)
	}
	p.cmds = append(p.cmds, cmd)
	return nil
}

// Len returns the number of queued commands.
func (p *Pipeline) Len() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.cmds)
}

// Exec flushes all queued commands in a single network call and
// returns one result per command. The command buffer is cleared
// regardless of success so callers can reuse the pipeline.
func (p *Pipeline) Exec(ctx context.Context) ([]PipelineResult, error) {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return nil, ErrPipelineClosed
	}
	if len(p.cmds) == 0 {
		p.mu.Unlock()
		return nil, ErrPipelineEmpty
	}
	batch := make([]PipelineCmd, len(p.cmds))
	copy(batch, p.cmds)
	p.cmds = p.cmds[:0]
	p.mu.Unlock()

	return p.flush(ctx, batch)
}

// Close prevents further commands from being added.
func (p *Pipeline) Close() {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.closed = true
	p.cmds = nil
}

// BatchGet is a convenience for batching multiple GET operations.
// Returns a map[key]->value for keys that were found.
func BatchGet(ctx context.Context, pipe *Pipeline, keys []string) (map[string]any, error) {
	for _, k := range keys {
		if err := pipe.Add(PipelineCmd{Op: "GET", Key: k}); err != nil {
			return nil, err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(keys))
	var partialErr error
	for i, r := range results {
		if r.Err != nil {
			partialErr = fmt.Errorf("%w: key=%s: %v", ErrPartialFailure, keys[i], r.Err)
			continue
		}
		if r.Value != nil {
			out[keys[i]] = r.Value
		}
	}
	return out, partialErr
}

// BatchSet is a convenience for batching multiple SET operations.
func BatchSet(ctx context.Context, pipe *Pipeline, entries map[string]any, ttl time.Duration) error {
	for k, v := range entries {
		if err := pipe.Add(PipelineCmd{Op: "SET", Key: k, Args: []any{v, ttl}}); err != nil {
			return err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	for i, r := range results {
		if r.Err != nil {
			return fmt.Errorf("%w: cmd=%d: %v", ErrPartialFailure, i, r.Err)
		}
	}
	return nil
}
