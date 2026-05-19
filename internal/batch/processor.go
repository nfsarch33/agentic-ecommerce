package batch

import (
	"context"
	"errors"
	"fmt"
)

var ErrCancelled = errors.New("batch: processing cancelled")

type ProcessFunc func(ctx context.Context, chunk []any) error

type ChunkResult struct {
	Index int
	Error error
}

type BatchResult struct {
	Total     int
	Completed int
	Failed    []ChunkResult
}

type ProgressReport struct {
	Total     int
	Completed int
	Failed    int
	PctDone   float64
}

// Chunk splits items into batches of the given size.
func Chunk(items []any, size int) [][]any {
	if size <= 0 || len(items) == 0 {
		return nil
	}
	var chunks [][]any
	for i := 0; i < len(items); i += size {
		end := i + size
		if end > len(items) {
			end = len(items)
		}
		chunks = append(chunks, items[i:end])
	}
	return chunks
}

// Process executes fn on each chunk, collecting per-chunk results.
func Process(ctx context.Context, chunks [][]any, fn ProcessFunc) BatchResult {
	result := BatchResult{Total: len(chunks)}
	for i, chunk := range chunks {
		select {
		case <-ctx.Done():
			result.Failed = append(result.Failed, ChunkResult{Index: i, Error: ErrCancelled})
			for j := i + 1; j < len(chunks); j++ {
				result.Failed = append(result.Failed, ChunkResult{Index: j, Error: ErrCancelled})
			}
			return result
		default:
		}
		if err := fn(ctx, chunk); err != nil {
			result.Failed = append(result.Failed, ChunkResult{Index: i, Error: err})
		} else {
			result.Completed++
		}
	}
	return result
}

// Retry re-runs fn on previously failed chunks.
func Retry(ctx context.Context, failed []ChunkResult, chunks [][]any, fn ProcessFunc) BatchResult {
	result := BatchResult{Total: len(failed)}
	for _, cr := range failed {
		if cr.Index >= len(chunks) {
			result.Failed = append(result.Failed, ChunkResult{
				Index: cr.Index,
				Error: fmt.Errorf("chunk index %d out of range", cr.Index),
			})
			continue
		}
		if err := fn(ctx, chunks[cr.Index]); err != nil {
			result.Failed = append(result.Failed, ChunkResult{Index: cr.Index, Error: err})
		} else {
			result.Completed++
		}
	}
	return result
}

// Progress summarises a BatchResult.
func Progress(result BatchResult) ProgressReport {
	pr := ProgressReport{
		Total:     result.Total,
		Completed: result.Completed,
		Failed:    len(result.Failed),
	}
	if result.Total > 0 {
		pr.PctDone = float64(result.Completed) / float64(result.Total) * 100
	}
	return pr
}
