// Package csvexport provides streaming CSV export with large-dataset chunking and async job tracking.
package csvexport

import (
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"io"
	"sync"
	"time"
)

// ErrJobNotFound is returned when a job ID is not present in the registry.
var ErrJobNotFound = errors.New("csvexport: job not found")

// Status values for async export jobs.
const (
	StatusPending    = "pending"
	StatusProcessing = "processing"
	StatusDone       = "done"
	StatusFailed     = "failed"
)

// Row is one CSV data row.
type Row []string

// DataSource is a function that streams rows into the given channel.
// It must close the channel when all rows have been emitted.
type DataSource func(ctx context.Context, rows chan<- Row) error

// WriterConfig controls the streaming writer.
type WriterConfig struct {
	Headers   []string
	ChunkSize int // rows per flush; 0 = flush every row
}

// StreamCSV writes rows from source to dst using the given config.
func StreamCSV(ctx context.Context, dst io.Writer, cfg WriterConfig, source DataSource) (int, error) {
	w := csv.NewWriter(dst)
	if len(cfg.Headers) > 0 {
		if err := w.Write(cfg.Headers); err != nil {
			return 0, fmt.Errorf("csvexport: write header: %w", err)
		}
	}

	rows := make(chan Row, 64)
	errCh := make(chan error, 1)
	go func() {
		errCh <- source(ctx, rows)
	}()

	chunkSize := cfg.ChunkSize
	if chunkSize <= 0 {
		chunkSize = 1
	}

	count := 0
	for row := range rows {
		if err := w.Write(row); err != nil {
			return count, fmt.Errorf("csvexport: write row: %w", err)
		}
		count++
		if count%chunkSize == 0 {
			w.Flush()
			if err := w.Error(); err != nil {
				return count, err
			}
		}
	}
	w.Flush()
	if err := w.Error(); err != nil {
		return count, err
	}
	if err := <-errCh; err != nil {
		return count, fmt.Errorf("csvexport: source error: %w", err)
	}
	return count, nil
}

// Job represents an async export task.
type Job struct {
	ID        string
	Status    string
	StartedAt time.Time
	DoneAt    *time.Time
	RowCount  int
	Error     string
}

// JobRegistry manages async export jobs.
type JobRegistry struct {
	mu   sync.RWMutex
	jobs map[string]*Job
	seq  int
}

// NewJobRegistry returns an empty registry.
func NewJobRegistry() *JobRegistry {
	return &JobRegistry{jobs: make(map[string]*Job)}
}

// Submit registers a new job and returns its ID.
func (r *JobRegistry) Submit(ctx context.Context, cfg WriterConfig, source DataSource, dst io.Writer) string {
	r.mu.Lock()
	r.seq++
	id := fmt.Sprintf("job-%d", r.seq)
	job := &Job{ID: id, Status: StatusPending, StartedAt: time.Now()}
	r.jobs[id] = job
	r.mu.Unlock()

	go func() {
		r.mu.Lock()
		job.Status = StatusProcessing
		r.mu.Unlock()

		n, err := StreamCSV(ctx, dst, cfg, source)

		r.mu.Lock()
		t := time.Now()
		job.DoneAt = &t
		job.RowCount = n
		if err != nil {
			job.Status = StatusFailed
			job.Error = err.Error()
		} else {
			job.Status = StatusDone
		}
		r.mu.Unlock()
	}()

	return id
}

// Status returns the current job state.
func (r *JobRegistry) Status(id string) (*Job, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	j, ok := r.jobs[id]
	if !ok {
		return nil, ErrJobNotFound
	}
	// Return a copy to avoid data races.
	cp := *j
	return &cp, nil
}

// WaitFor polls the job until it leaves the processing state or the context expires.
func (r *JobRegistry) WaitFor(ctx context.Context, id string, poll time.Duration) (*Job, error) {
	if poll <= 0 {
		poll = 5 * time.Millisecond
	}
	for {
		j, err := r.Status(id)
		if err != nil {
			return nil, err
		}
		if j.Status == StatusDone || j.Status == StatusFailed {
			return j, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(poll):
		}
	}
}
