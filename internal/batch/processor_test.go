package batch_test

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/batch"
)

func TestBatch_ChunkSplitsCorrectly(t *testing.T) {
	t.Parallel()
	items := []any{1, 2, 3, 4, 5}
	chunks := batch.Chunk(items, 2)
	if len(chunks) != 3 {
		t.Fatalf("expected 3 chunks, got %d", len(chunks))
	}
	if len(chunks[2]) != 1 {
		t.Fatalf("expected last chunk size 1, got %d", len(chunks[2]))
	}
}

func TestBatch_ProcessAllChunks(t *testing.T) {
	t.Parallel()
	chunks := [][]any{{1}, {2}, {3}}
	result := batch.Process(context.Background(), chunks, func(_ context.Context, _ []any) error { return nil })
	if result.Completed != 3 {
		t.Fatalf("expected 3 completed, got %d", result.Completed)
	}
	if len(result.Failed) != 0 {
		t.Fatalf("expected 0 failed, got %d", len(result.Failed))
	}
}

func TestBatch_FailedChunksRecorded(t *testing.T) {
	t.Parallel()
	errTest := errors.New("chunk error")
	chunks := [][]any{{1}, {2}, {3}}
	result := batch.Process(context.Background(), chunks, func(_ context.Context, chunk []any) error {
		if chunk[0] == 2 {
			return errTest
		}
		return nil
	})
	if result.Completed != 2 {
		t.Fatalf("expected 2 completed, got %d", result.Completed)
	}
	if len(result.Failed) != 1 {
		t.Fatalf("expected 1 failed, got %d", len(result.Failed))
	}
}

func TestBatch_RetryFailedChunks(t *testing.T) {
	t.Parallel()
	chunks := [][]any{{1}, {2}}
	failed := []batch.ChunkResult{{Index: 0, Error: errors.New("retry me")}}
	result := batch.Retry(context.Background(), failed, chunks, func(_ context.Context, _ []any) error { return nil })
	if result.Completed != 1 {
		t.Fatalf("expected 1 completed after retry, got %d", result.Completed)
	}
}

func TestBatch_ProgressReportsAccurately(t *testing.T) {
	t.Parallel()
	result := batch.BatchResult{Total: 10, Completed: 7, Failed: []batch.ChunkResult{{Index: 0}}}
	pr := batch.Progress(result)
	if pr.PctDone < 69 || pr.PctDone > 71 {
		t.Fatalf("expected ~70%%, got %f", pr.PctDone)
	}
	if pr.Failed != 1 {
		t.Fatalf("expected 1 failed, got %d", pr.Failed)
	}
}

func TestBatch_CancelStopsRemaining(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // pre-cancel
	chunks := [][]any{{1}, {2}, {3}}
	result := batch.Process(ctx, chunks, func(_ context.Context, _ []any) error { return nil })
	if result.Completed != 0 {
		t.Fatalf("expected 0 completed after cancel, got %d", result.Completed)
	}
}

func TestBatch_EmptyInputHandled(t *testing.T) {
	t.Parallel()
	chunks := batch.Chunk(nil, 5)
	if len(chunks) != 0 {
		t.Fatalf("expected empty chunks, got %d", len(chunks))
	}
	result := batch.Process(context.Background(), nil, func(_ context.Context, _ []any) error { return nil })
	if result.Total != 0 {
		t.Fatal("expected 0 total for empty input")
	}
}
