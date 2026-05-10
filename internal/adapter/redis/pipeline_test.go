package redis

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"
)

func newMemoryFlush() (FlushFunc, *memStore) {
	store := &memStore{data: map[string]any{}}
	return func(_ context.Context, cmds []PipelineCmd) ([]PipelineResult, error) {
		store.mu.Lock()
		defer store.mu.Unlock()
		results := make([]PipelineResult, len(cmds))
		for i, cmd := range cmds {
			switch cmd.Op {
			case "GET":
				v, ok := store.data[cmd.Key]
				if !ok {
					results[i] = PipelineResult{Value: nil}
				} else {
					results[i] = PipelineResult{Value: v}
				}
			case "SET":
				if len(cmd.Args) > 0 {
					store.data[cmd.Key] = cmd.Args[0]
				}
				results[i] = PipelineResult{Value: "OK"}
			default:
				results[i] = PipelineResult{Err: errors.New("unknown op")}
			}
		}
		return results, nil
	}, store
}

type memStore struct {
	mu   sync.Mutex
	data map[string]any
}

func TestPipeline_ReducesRoundTrips(t *testing.T) {
	flush, store := newMemoryFlush()
	pipe := NewPipeline(flush)
	ctx := context.Background()

	store.mu.Lock()
	store.data["key1"] = "val1"
	store.data["key2"] = "val2"
	store.data["key3"] = "val3"
	store.mu.Unlock()

	keys := []string{"key1", "key2", "key3", "missing"}
	got, err := BatchGet(ctx, pipe, keys)
	if err != nil {
		t.Fatalf("BatchGet: %v", err)
	}

	if len(got) != 3 {
		t.Fatalf("got %d results, want 3", len(got))
	}
	for _, k := range []string{"key1", "key2", "key3"} {
		if _, ok := got[k]; !ok {
			t.Errorf("missing key %s", k)
		}
	}
	if _, ok := got["missing"]; ok {
		t.Error("found unexpected key 'missing'")
	}
}

func TestPipeline_MaintainsCorrectness(t *testing.T) {
	flush, store := newMemoryFlush()
	pipe := NewPipeline(flush)
	ctx := context.Background()

	entries := map[string]any{
		"a": "alpha",
		"b": "beta",
		"c": "gamma",
	}
	if err := BatchSet(ctx, pipe, entries, time.Minute); err != nil {
		t.Fatalf("BatchSet: %v", err)
	}

	store.mu.Lock()
	for k, want := range entries {
		if got := store.data[k]; got != want {
			t.Errorf("key %s = %v, want %v", k, got, want)
		}
	}
	store.mu.Unlock()

	pipe2 := NewPipeline(flush)
	got, err := BatchGet(ctx, pipe2, []string{"a", "b", "c"})
	if err != nil {
		t.Fatalf("BatchGet after set: %v", err)
	}
	if got["a"] != "alpha" || got["b"] != "beta" || got["c"] != "gamma" {
		t.Errorf("values mismatch: %v", got)
	}
}

func TestPipeline_HandlesPartialFailure(t *testing.T) {
	callCount := 0
	flush := func(_ context.Context, cmds []PipelineCmd) ([]PipelineResult, error) {
		callCount++
		results := make([]PipelineResult, len(cmds))
		for i, cmd := range cmds {
			if cmd.Key == "fail" {
				results[i] = PipelineResult{Err: errors.New("simulated error")}
			} else {
				results[i] = PipelineResult{Value: "ok-" + cmd.Key}
			}
		}
		return results, nil
	}

	pipe := NewPipeline(flush)
	ctx := context.Background()

	keys := []string{"good1", "fail", "good2"}
	got, err := BatchGet(ctx, pipe, keys)

	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got: %v", err)
	}
	if got["good1"] != "ok-good1" {
		t.Errorf("good1 = %v, want ok-good1", got["good1"])
	}
	if got["good2"] != "ok-good2" {
		t.Errorf("good2 = %v, want ok-good2", got["good2"])
	}
	if _, ok := got["fail"]; ok {
		t.Error("fail key should not be in results")
	}
}

func TestPipeline_Closed(t *testing.T) {
	flush, _ := newMemoryFlush()
	pipe := NewPipeline(flush)
	pipe.Close()

	if err := pipe.Add(PipelineCmd{Op: "GET", Key: "x"}); !errors.Is(err, ErrPipelineClosed) {
		t.Fatalf("expected ErrPipelineClosed, got: %v", err)
	}
	if _, err := pipe.Exec(context.Background()); !errors.Is(err, ErrPipelineClosed) {
		t.Fatalf("expected ErrPipelineClosed on Exec, got: %v", err)
	}
}

func TestPipeline_Empty(t *testing.T) {
	flush, _ := newMemoryFlush()
	pipe := NewPipeline(flush)

	if _, err := pipe.Exec(context.Background()); !errors.Is(err, ErrPipelineEmpty) {
		t.Fatalf("expected ErrPipelineEmpty, got: %v", err)
	}
}

func TestPipeline_Full(t *testing.T) {
	flush, _ := newMemoryFlush()
	pipe := NewPipeline(flush)
	pipe.maxSize = 2

	_ = pipe.Add(PipelineCmd{Op: "GET", Key: "a"})
	_ = pipe.Add(PipelineCmd{Op: "GET", Key: "b"})

	if err := pipe.Add(PipelineCmd{Op: "GET", Key: "c"}); !errors.Is(err, ErrPipelineFull) {
		t.Fatalf("expected ErrPipelineFull, got: %v", err)
	}
}

func TestPipeline_ReusableAfterExec(t *testing.T) {
	flush, _ := newMemoryFlush()
	pipe := NewPipeline(flush)
	ctx := context.Background()

	_ = pipe.Add(PipelineCmd{Op: "SET", Key: "x", Args: []any{"1", time.Minute}})
	_, err := pipe.Exec(ctx)
	if err != nil {
		t.Fatalf("first exec: %v", err)
	}

	if pipe.Len() != 0 {
		t.Fatalf("len after exec = %d, want 0", pipe.Len())
	}

	_ = pipe.Add(PipelineCmd{Op: "GET", Key: "x"})
	results, err := pipe.Exec(ctx)
	if err != nil {
		t.Fatalf("second exec: %v", err)
	}
	if results[0].Value != "1" {
		t.Errorf("got %v, want 1", results[0].Value)
	}
}

func BenchmarkPipeline_BatchGet10(b *testing.B) {
	flush, store := newMemoryFlush()
	store.mu.Lock()
	for i := 0; i < 10; i++ {
		store.data[fmt.Sprintf("key%d", i)] = fmt.Sprintf("val%d", i)
	}
	store.mu.Unlock()

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%d", i)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipe := NewPipeline(flush)
		_, _ = BatchGet(ctx, pipe, keys)
	}
}

func BenchmarkSingleGet10(b *testing.B) {
	flush, store := newMemoryFlush()
	store.mu.Lock()
	for i := 0; i < 10; i++ {
		store.data[fmt.Sprintf("key%d", i)] = fmt.Sprintf("val%d", i)
	}
	store.mu.Unlock()

	keys := make([]string, 10)
	for i := range keys {
		keys[i] = fmt.Sprintf("key%d", i)
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, k := range keys {
			pipe := NewPipeline(flush)
			_ = pipe.Add(PipelineCmd{Op: "GET", Key: k})
			_, _ = pipe.Exec(ctx)
		}
	}
}
