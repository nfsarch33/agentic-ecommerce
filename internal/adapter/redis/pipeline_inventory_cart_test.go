package redis

// File scope: v6.3.0 Pair 3 MVP — Story 4 Redis pipeline expansion.
//
// Adds 6 pipelined operations targeting inventory reservation +
// cart aggregation hot paths. Tests use the in-memory FlushFunc so
// they stay deterministic and run in the default suite. The
// integration test file (build tag `integration_redis`) exercises
// the same surface against a real testcontainers-go Redis.
//
// New ops (all flushed in a single round-trip):
//   1. BatchHSet            -> set multiple hash fields in one call
//   2. BatchHGet            -> read multiple hash fields in one call
//   3. BatchIncrBy          -> atomic increment for many counters
//   4. BatchExpire          -> TTL stamp many keys
//   5. BatchDel             -> bulk eviction
//   6. ReserveInventoryBatch -> domain helper: DECRBY + EXPIRE per SKU
//   7. CartAggregateBatch    -> domain helper: HGETALL across cart keys
//
// Coverage targets: each op exercises the happy path + a
// partial-failure path so ErrPartialFailure surfaces correctly.

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"
)

// memHashStore extends memStore semantics with hash + counter ops so
// the in-memory FlushFunc covers the new surface without dragging in
// a Redis client.
type memHashStore struct {
	mu       sync.Mutex
	values   map[string]any
	hashes   map[string]map[string]any
	counters map[string]int64
	expires  map[string]time.Duration
	failKeys map[string]bool
}

func newMemHashStore() *memHashStore {
	return &memHashStore{
		values:   map[string]any{},
		hashes:   map[string]map[string]any{},
		counters: map[string]int64{},
		expires:  map[string]time.Duration{},
		failKeys: map[string]bool{},
	}
}

// flush executes one batch atomically against the in-memory store.
// Returns one PipelineResult per command (preserving order).
func (s *memHashStore) flush(_ context.Context, cmds []PipelineCmd) ([]PipelineResult, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	results := make([]PipelineResult, len(cmds))
	for i, cmd := range cmds {
		if s.failKeys[cmd.Key] {
			results[i] = PipelineResult{Err: fmt.Errorf("simulated key failure: %s", cmd.Key)}
			continue
		}
		results[i] = s.applyOp(cmd)
	}
	return results, nil
}

func (s *memHashStore) applyOp(cmd PipelineCmd) PipelineResult {
	switch cmd.Op {
	case "HSET":
		return s.opHSet(cmd)
	case "HGET":
		return s.opHGet(cmd)
	case "HGETALL":
		return s.opHGetAll(cmd)
	case "INCRBY":
		return s.opIncrBy(cmd)
	case "EXPIRE":
		return s.opExpire(cmd)
	case "DEL":
		return s.opDel(cmd)
	case "DECRBY":
		return s.opDecrBy(cmd)
	default:
		return PipelineResult{Err: errors.New("unknown op: " + cmd.Op)}
	}
}

func (s *memHashStore) opHSet(cmd PipelineCmd) PipelineResult {
	if len(cmd.Args) < 2 {
		return PipelineResult{Err: errors.New("HSET requires field+value")}
	}
	field, _ := cmd.Args[0].(string)
	if _, ok := s.hashes[cmd.Key]; !ok {
		s.hashes[cmd.Key] = map[string]any{}
	}
	s.hashes[cmd.Key][field] = cmd.Args[1]
	return PipelineResult{Value: int64(1)}
}

func (s *memHashStore) opHGet(cmd PipelineCmd) PipelineResult {
	if len(cmd.Args) < 1 {
		return PipelineResult{Err: errors.New("HGET requires field")}
	}
	field, _ := cmd.Args[0].(string)
	if h, ok := s.hashes[cmd.Key]; ok {
		if v, ok := h[field]; ok {
			return PipelineResult{Value: v}
		}
	}
	return PipelineResult{Value: nil}
}

func (s *memHashStore) opHGetAll(cmd PipelineCmd) PipelineResult {
	if h, ok := s.hashes[cmd.Key]; ok {
		out := make(map[string]any, len(h))
		for k, v := range h {
			out[k] = v
		}
		return PipelineResult{Value: out}
	}
	return PipelineResult{Value: map[string]any{}}
}

func (s *memHashStore) opIncrBy(cmd PipelineCmd) PipelineResult {
	if len(cmd.Args) < 1 {
		return PipelineResult{Err: errors.New("INCRBY requires delta")}
	}
	delta, ok := cmd.Args[0].(int64)
	if !ok {
		return PipelineResult{Err: errors.New("INCRBY delta must be int64")}
	}
	s.counters[cmd.Key] += delta
	return PipelineResult{Value: s.counters[cmd.Key]}
}

func (s *memHashStore) opDecrBy(cmd PipelineCmd) PipelineResult {
	if len(cmd.Args) < 1 {
		return PipelineResult{Err: errors.New("DECRBY requires delta")}
	}
	delta, ok := cmd.Args[0].(int64)
	if !ok {
		return PipelineResult{Err: errors.New("DECRBY delta must be int64")}
	}
	s.counters[cmd.Key] -= delta
	return PipelineResult{Value: s.counters[cmd.Key]}
}

func (s *memHashStore) opExpire(cmd PipelineCmd) PipelineResult {
	if len(cmd.Args) < 1 {
		return PipelineResult{Err: errors.New("EXPIRE requires ttl")}
	}
	ttl, ok := cmd.Args[0].(time.Duration)
	if !ok {
		return PipelineResult{Err: errors.New("EXPIRE ttl must be duration")}
	}
	s.expires[cmd.Key] = ttl
	return PipelineResult{Value: int64(1)}
}

func (s *memHashStore) opDel(_ PipelineCmd) PipelineResult {
	return PipelineResult{Value: int64(1)}
}

// --- BatchHSet --------------------------------------------------------

func TestBatchHSet_RoundTripsAllFields(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	entries := map[string]map[string]any{
		"cart:t1:c1": {"sku-a": int64(2), "sku-b": int64(1)},
		"cart:t1:c2": {"sku-a": int64(5)},
	}
	if err := BatchHSet(ctx, pipe, entries); err != nil {
		t.Fatalf("BatchHSet: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	if got := store.hashes["cart:t1:c1"]["sku-a"]; got != int64(2) {
		t.Fatalf("cart:t1:c1 sku-a = %v, want 2", got)
	}
	if got := store.hashes["cart:t1:c1"]["sku-b"]; got != int64(1) {
		t.Fatalf("cart:t1:c1 sku-b = %v, want 1", got)
	}
	if got := store.hashes["cart:t1:c2"]["sku-a"]; got != int64(5) {
		t.Fatalf("cart:t1:c2 sku-a = %v, want 5", got)
	}
}

func TestBatchHSet_EmptyEntries_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	ctx := context.Background()
	if err := BatchHSet(ctx, pipe, nil); err != nil {
		t.Fatalf("nil entries should be no-op: %v", err)
	}
	if pipe.Len() != 0 {
		t.Fatalf("pipe should remain empty, got %d", pipe.Len())
	}
	_ = store
}

// --- BatchHGet --------------------------------------------------------

func TestBatchHGet_ReadsKnownFields(t *testing.T) {
	store := newMemHashStore()
	store.hashes["cart:t1:c1"] = map[string]any{"sku-a": int64(3), "sku-b": int64(7)}
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	got, err := BatchHGet(ctx, pipe, "cart:t1:c1", []string{"sku-a", "sku-b", "missing"})
	if err != nil {
		t.Fatalf("BatchHGet: %v", err)
	}
	if got["sku-a"] != int64(3) || got["sku-b"] != int64(7) {
		t.Fatalf("unexpected values: %+v", got)
	}
	if _, ok := got["missing"]; ok {
		t.Fatalf("missing field should not be present")
	}
}

// --- BatchIncrBy ------------------------------------------------------

func TestBatchIncrBy_AggregatesCounters(t *testing.T) {
	store := newMemHashStore()
	store.counters["counter:a"] = 0
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	deltas := map[string]int64{
		"counter:a": 5,
		"counter:b": 12,
	}
	got, err := BatchIncrBy(ctx, pipe, deltas)
	if err != nil {
		t.Fatalf("BatchIncrBy: %v", err)
	}
	if got["counter:a"] != 5 {
		t.Fatalf("counter:a = %d, want 5", got["counter:a"])
	}
	if got["counter:b"] != 12 {
		t.Fatalf("counter:b = %d, want 12", got["counter:b"])
	}
}

// --- BatchExpire ------------------------------------------------------

func TestBatchExpire_StampsTTLs(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	ttl := 30 * time.Second
	keys := []string{"reserve:t1:o1:sku-a", "reserve:t1:o1:sku-b"}
	if err := BatchExpire(ctx, pipe, keys, ttl); err != nil {
		t.Fatalf("BatchExpire: %v", err)
	}
	store.mu.Lock()
	defer store.mu.Unlock()
	for _, k := range keys {
		if store.expires[k] != ttl {
			t.Fatalf("expire(%s) = %v, want %v", k, store.expires[k], ttl)
		}
	}
}

// --- BatchDel ---------------------------------------------------------

func TestBatchDel_RemovesAllKeys(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	if err := BatchDel(ctx, pipe, []string{"cart:t1:c1", "cart:t1:c2"}); err != nil {
		t.Fatalf("BatchDel: %v", err)
	}
}

// --- ReserveInventoryBatch -------------------------------------------

func TestReserveInventoryBatch_DecrementsAndStampsTTL(t *testing.T) {
	store := newMemHashStore()
	store.counters["inv:t1:sku-a"] = 100
	store.counters["inv:t1:sku-b"] = 50
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	reservations := map[string]int64{
		"inv:t1:sku-a": 3,
		"inv:t1:sku-b": 2,
	}
	got, err := ReserveInventoryBatch(ctx, pipe, reservations, 5*time.Minute)
	if err != nil {
		t.Fatalf("ReserveInventoryBatch: %v", err)
	}
	if got["inv:t1:sku-a"] != 97 {
		t.Fatalf("inv:t1:sku-a = %d, want 97", got["inv:t1:sku-a"])
	}
	if got["inv:t1:sku-b"] != 48 {
		t.Fatalf("inv:t1:sku-b = %d, want 48", got["inv:t1:sku-b"])
	}
	store.mu.Lock()
	if store.expires["inv:t1:sku-a"] != 5*time.Minute {
		t.Fatalf("expire missing on sku-a")
	}
	store.mu.Unlock()
}

// --- CartAggregateBatch ----------------------------------------------

func TestCartAggregateBatch_AggregatesAcrossCarts(t *testing.T) {
	store := newMemHashStore()
	store.hashes["cart:t1:c1"] = map[string]any{"sku-a": int64(2), "sku-b": int64(1)}
	store.hashes["cart:t1:c2"] = map[string]any{"sku-a": int64(5)}
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	got, err := CartAggregateBatch(ctx, pipe, []string{"cart:t1:c1", "cart:t1:c2"})
	if err != nil {
		t.Fatalf("CartAggregateBatch: %v", err)
	}
	if got["cart:t1:c1"]["sku-a"] != int64(2) {
		t.Fatalf("c1 sku-a = %v", got["cart:t1:c1"]["sku-a"])
	}
	if got["cart:t1:c2"]["sku-a"] != int64(5) {
		t.Fatalf("c2 sku-a = %v", got["cart:t1:c2"]["sku-a"])
	}
}

// --- Partial failure surface -----------------------------------------

func TestNewPipelineOps_SurfacePartialFailure(t *testing.T) {
	store := newMemHashStore()
	store.failKeys["counter:bad"] = true
	pipe := NewPipeline(store.flush)
	ctx := context.Background()

	deltas := map[string]int64{"counter:good": 4, "counter:bad": 9}
	_, err := BatchIncrBy(ctx, pipe, deltas)
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
	if !strings.Contains(err.Error(), "counter:bad") {
		t.Fatalf("error should name failing key: %v", err)
	}
}

// --- Empty-input branches (cover the no-op early returns) -----------

func TestBatchHGet_EmptyFields_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	got, err := BatchHGet(context.Background(), pipe, "k", nil)
	if err != nil {
		t.Fatalf("BatchHGet nil: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

func TestBatchIncrBy_EmptyDeltas_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	got, err := BatchIncrBy(context.Background(), pipe, nil)
	if err != nil {
		t.Fatalf("BatchIncrBy nil: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

func TestBatchExpire_EmptyKeys_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	if err := BatchExpire(context.Background(), pipe, nil, time.Minute); err != nil {
		t.Fatalf("BatchExpire nil: %v", err)
	}
}

func TestBatchDel_EmptyKeys_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	if err := BatchDel(context.Background(), pipe, nil); err != nil {
		t.Fatalf("BatchDel nil: %v", err)
	}
}

func TestReserveInventoryBatch_EmptyReservations_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	got, err := ReserveInventoryBatch(context.Background(), pipe, nil, time.Minute)
	if err != nil {
		t.Fatalf("Reserve nil: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

func TestCartAggregateBatch_EmptyKeys_NoOp(t *testing.T) {
	store := newMemHashStore()
	pipe := NewPipeline(store.flush)
	got, err := CartAggregateBatch(context.Background(), pipe, nil)
	if err != nil {
		t.Fatalf("Cart nil: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %d, want 0", len(got))
	}
}

// --- Partial failure surface for each helper ------------------------

func TestBatchHGet_PartialFailure(t *testing.T) {
	store := newMemHashStore()
	store.hashes["cart:t1"] = map[string]any{"sku-a": int64(1)}
	store.failKeys["cart:t1"] = true
	pipe := NewPipeline(store.flush)
	_, err := BatchHGet(context.Background(), pipe, "cart:t1", []string{"sku-a"})
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
}

func TestBatchHSet_PartialFailure(t *testing.T) {
	store := newMemHashStore()
	store.failKeys["cart:bad"] = true
	pipe := NewPipeline(store.flush)
	err := BatchHSet(context.Background(), pipe, map[string]map[string]any{
		"cart:bad": {"sku": int64(1)},
	})
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
}

func TestBatchExpire_PartialFailure(t *testing.T) {
	store := newMemHashStore()
	store.failKeys["bad-key"] = true
	pipe := NewPipeline(store.flush)
	err := BatchExpire(context.Background(), pipe, []string{"bad-key"}, time.Minute)
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
}

func TestBatchDel_PartialFailure(t *testing.T) {
	store := newMemHashStore()
	store.failKeys["bad-key"] = true
	pipe := NewPipeline(store.flush)
	err := BatchDel(context.Background(), pipe, []string{"bad-key"})
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
}

func TestCartAggregateBatch_PartialFailure(t *testing.T) {
	store := newMemHashStore()
	store.hashes["cart:t1"] = map[string]any{"sku-a": int64(1)}
	store.failKeys["cart:t1"] = true
	pipe := NewPipeline(store.flush)
	_, err := CartAggregateBatch(context.Background(), pipe, []string{"cart:t1"})
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
}

func TestReserveInventoryBatch_PartialFailure_DECRBY(t *testing.T) {
	store := newMemHashStore()
	store.failKeys["inv:bad"] = true
	pipe := NewPipeline(store.flush)
	_, err := ReserveInventoryBatch(context.Background(), pipe, map[string]int64{"inv:bad": 1}, time.Minute)
	if !errors.Is(err, ErrPartialFailure) {
		t.Fatalf("expected ErrPartialFailure, got %v", err)
	}
}

// --- Bench ------------------------------------------------------------

func BenchmarkReserveInventoryBatch_50SKUs(b *testing.B) {
	store := newMemHashStore()
	reservations := make(map[string]int64, 50)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("inv:t1:sku-%d", i)
		store.counters[key] = 100
		reservations[key] = 1
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		pipe := NewPipeline(store.flush)
		_, _ = ReserveInventoryBatch(ctx, pipe, reservations, time.Minute)
	}
}

func BenchmarkReserveInventorySingles_50SKUs(b *testing.B) {
	store := newMemHashStore()
	reservations := make(map[string]int64, 50)
	for i := 0; i < 50; i++ {
		key := fmt.Sprintf("inv:t1:sku-%d", i)
		store.counters[key] = 100
		reservations[key] = 1
	}
	ctx := context.Background()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for k, v := range reservations {
			pipe := NewPipeline(store.flush)
			_ = pipe.Add(PipelineCmd{Op: "DECRBY", Key: k, Args: []any{v}})
			_, _ = pipe.Exec(ctx)
			pipe2 := NewPipeline(store.flush)
			_ = pipe2.Add(PipelineCmd{Op: "EXPIRE", Key: k, Args: []any{time.Minute}})
			_, _ = pipe2.Exec(ctx)
		}
	}
}
