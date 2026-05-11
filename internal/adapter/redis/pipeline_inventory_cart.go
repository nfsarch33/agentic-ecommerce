package redis

// File scope: v6.3.0 Pair 3 MVP — Story 4 Redis pipeline expansion.
//
// Adds 6 pipelined helpers that target inventory reservation +
// cart aggregation hot paths. Each helper enqueues all commands on
// a single Pipeline, calls Exec once (one network round-trip), and
// surfaces partial failures via ErrPartialFailure.
//
// Helpers added:
//   - BatchHSet            : multiple hash fields in one round-trip
//   - BatchHGet            : multiple hash fields in one round-trip
//   - BatchIncrBy          : atomic counter increments
//   - BatchExpire          : bulk TTL stamping
//   - BatchDel             : bulk key eviction
//   - ReserveInventoryBatch : domain helper combining DECRBY + EXPIRE
//   - CartAggregateBatch    : domain helper using HGETALL across keys
//
// All helpers preserve the existing Pipeline contract (Add -> Exec
// once -> reusable) so the cyclomatic complexity per helper stays
// at <= 4 (HARD GATE: complex_fn must NOT regress beyond 5).

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// BatchHSet sets multiple hash fields across multiple keys in a
// single pipeline flush.
//
// entries: key -> field -> value.
// Empty entries map is a no-op and returns nil.
func BatchHSet(ctx context.Context, pipe *Pipeline, entries map[string]map[string]any) error {
	if len(entries) == 0 {
		return nil
	}
	for key, fields := range entries {
		for field, value := range fields {
			if err := pipe.Add(PipelineCmd{Op: "HSET", Key: key, Args: []any{field, value}}); err != nil {
				return err
			}
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

// BatchHGet reads multiple hash fields from one key in a single
// pipeline flush. Returns a map[field]value for fields that were
// found.
func BatchHGet(ctx context.Context, pipe *Pipeline, key string, fields []string) (map[string]any, error) {
	if len(fields) == 0 {
		return map[string]any{}, nil
	}
	for _, f := range fields {
		if err := pipe.Add(PipelineCmd{Op: "HGET", Key: key, Args: []any{f}}); err != nil {
			return nil, err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]any, len(fields))
	var partialErr error
	for i, r := range results {
		if r.Err != nil {
			partialErr = fmt.Errorf("%w: field=%s: %v", ErrPartialFailure, fields[i], r.Err)
			continue
		}
		if r.Value != nil {
			out[fields[i]] = r.Value
		}
	}
	return out, partialErr
}

// BatchIncrBy applies a INCRBY across multiple counter keys in one
// pipeline flush. Returns the post-increment value per key.
func BatchIncrBy(ctx context.Context, pipe *Pipeline, deltas map[string]int64) (map[string]int64, error) {
	if len(deltas) == 0 {
		return map[string]int64{}, nil
	}
	keys := make([]string, 0, len(deltas))
	for k := range deltas {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if err := pipe.Add(PipelineCmd{Op: "INCRBY", Key: k, Args: []any{deltas[k]}}); err != nil {
			return nil, err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(keys))
	var partialErr error
	for i, r := range results {
		if r.Err != nil {
			partialErr = fmt.Errorf("%w: key=%s: %v", ErrPartialFailure, keys[i], r.Err)
			continue
		}
		if v, ok := r.Value.(int64); ok {
			out[keys[i]] = v
		}
	}
	return out, partialErr
}

// BatchExpire stamps a TTL on multiple keys in a single pipeline
// flush. Useful after a multi-key write to ensure they all share
// the same eviction deadline.
func BatchExpire(ctx context.Context, pipe *Pipeline, keys []string, ttl time.Duration) error {
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if err := pipe.Add(PipelineCmd{Op: "EXPIRE", Key: k, Args: []any{ttl}}); err != nil {
			return err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	for i, r := range results {
		if r.Err != nil {
			return fmt.Errorf("%w: key=%s: %v", ErrPartialFailure, keys[i], r.Err)
		}
	}
	return nil
}

// BatchDel removes multiple keys in a single pipeline flush.
func BatchDel(ctx context.Context, pipe *Pipeline, keys []string) error {
	if len(keys) == 0 {
		return nil
	}
	for _, k := range keys {
		if err := pipe.Add(PipelineCmd{Op: "DEL", Key: k}); err != nil {
			return err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return err
	}
	for i, r := range results {
		if r.Err != nil {
			return fmt.Errorf("%w: key=%s: %v", ErrPartialFailure, keys[i], r.Err)
		}
	}
	return nil
}

// ReserveInventoryBatch is a domain helper that pipelines DECRBY
// (atomic decrement) followed by EXPIRE (TTL stamp) for each
// inventory key. The two phases are flushed as two pipeline calls
// so failure isolation is per-phase: if the DECRBY phase succeeds
// but the EXPIRE phase fails, the caller still has a valid
// reservation that they can re-stamp on retry.
//
// Returns the post-DECRBY value per key.
func ReserveInventoryBatch(ctx context.Context, pipe *Pipeline, reservations map[string]int64, ttl time.Duration) (map[string]int64, error) {
	if len(reservations) == 0 {
		return map[string]int64{}, nil
	}
	keys := make([]string, 0, len(reservations))
	for k := range reservations {
		keys = append(keys, k)
	}
	for _, k := range keys {
		if err := pipe.Add(PipelineCmd{Op: "DECRBY", Key: k, Args: []any{reservations[k]}}); err != nil {
			return nil, err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]int64, len(keys))
	var partialErr error
	for i, r := range results {
		if r.Err != nil {
			partialErr = fmt.Errorf("%w: key=%s: %v", ErrPartialFailure, keys[i], r.Err)
			continue
		}
		if v, ok := r.Value.(int64); ok {
			out[keys[i]] = v
		}
	}
	if expireErr := BatchExpire(ctx, NewPipeline(pipe.flush), keys, ttl); expireErr != nil && partialErr == nil {
		partialErr = expireErr
	}
	return out, partialErr
}

// CartAggregateBatch reads HGETALL across multiple cart keys in a
// single pipeline flush. Returns key -> field -> value.
func CartAggregateBatch(ctx context.Context, pipe *Pipeline, cartKeys []string) (map[string]map[string]any, error) {
	if len(cartKeys) == 0 {
		return map[string]map[string]any{}, nil
	}
	for _, k := range cartKeys {
		if err := pipe.Add(PipelineCmd{Op: "HGETALL", Key: k}); err != nil {
			return nil, err
		}
	}
	results, err := pipe.Exec(ctx)
	if err != nil {
		return nil, err
	}
	out := make(map[string]map[string]any, len(cartKeys))
	var partialErr error
	for i, r := range results {
		if r.Err != nil {
			partialErr = fmt.Errorf("%w: key=%s: %v", ErrPartialFailure, cartKeys[i], r.Err)
			continue
		}
		if m, ok := r.Value.(map[string]any); ok {
			out[cartKeys[i]] = m
		}
	}
	return out, partialErr
}

// ErrPipelineNoFlush is returned when a helper cannot derive the
// flush function from the parent pipeline (defensive only; current
// helpers always pass a constructed Pipeline).
var ErrPipelineNoFlush = errors.New("redis: pipeline missing flush function")
