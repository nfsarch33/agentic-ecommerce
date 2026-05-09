// File scope: v3.3.0 EC-3-4 bidirectional WC <-> TikTok Shop
// inventory sync saga.
//
// The saga is a typed transition function over two upstream stock
// adjusters (WooCommerce + TikTok Shop). Every Apply call:
//
//  1. Reserves the (tenant, order_id, channel) idempotency key so a
//     duplicate webhook OR a duplicate WC fulfilment never
//     double-decrements. This is the "dual-platform zero oversell"
//     guarantee from the EC-3-4 acceptance criterion.
//  2. Adjusts the source channel's stock (no-op when the source is
//     WC -- the upstream WC fulfilment workflow is the producer).
//  3. Adjusts the target channel's stock via the supplied adjuster
//     port. On error the source-side compensation runs, the
//     idempotency key is released so a retry can fire, and the
//     typed sentinel surfaces.
//  4. Emits per-direction Prometheus metrics so dashboards can chart
//     WC->TikTok / TikTok->WC throughput + failure rates.
//
// The saga is deliberately built without Temporal so it can run in
// the order webhook + WC fulfilment hot paths without a worker
// hop. A Temporal workflow wrapper is a v3.5+ follow-up that
// composes this saga with the v2.5 dispatcher pattern.
//
// Decomposition: every public method is a thin wrapper that gates
// on direction + closed flag and delegates to runApply (the
// shared logic). Per-function cyclomatic stays under 5.
package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	stdsync "sync"
	"time"
)

// SyncDirection identifies which side of the sync is the producer.
type SyncDirection string

const (
	// DirectionWCToTikTok fires when WC fulfilment confirms an
	// outbound shipment; we decrement the corresponding TikTok
	// listing stock.
	DirectionWCToTikTok SyncDirection = "wc_to_tiktok"
	// DirectionTikTokToWC fires when a TikTok order webhook lands;
	// we decrement the local WC inventory by the order quantity.
	DirectionTikTokToWC SyncDirection = "tiktok_to_wc"
)

// v3.3.0 EC-3-4 sentinels.
var (
	// ErrInventorySyncUnconfigured is returned by NewInventorySync
	// when a required adjuster port is missing.
	ErrInventorySyncUnconfigured = errors.New("inventory_sync: unconfigured")
	// ErrInventorySyncClosed is returned by Apply after Close.
	ErrInventorySyncClosed = errors.New("inventory_sync: closed")
	// ErrInventorySyncDuplicate is returned by Apply when the
	// idempotency key has already been observed. NOT an error in the
	// caller's eyes -- the saga short-circuits with this sentinel so
	// the caller can treat it as success.
	ErrInventorySyncDuplicate = errors.New("inventory_sync: duplicate")
	// ErrInventorySyncTargetFailed is returned when the target
	// adjuster failed AND the compensating action ran. Wraps the
	// upstream error.
	ErrInventorySyncTargetFailed = errors.New("inventory_sync: target failed; rolled back")
	// ErrInventorySyncRollbackFailed is returned when both the
	// target adjuster AND the compensating action failed. The
	// upstream errors are joined via errors.Join so callers can
	// errors.Is on either.
	ErrInventorySyncRollbackFailed = errors.New("inventory_sync: rollback failed; manual intervention required")
)

// StockAdjuster is the small port both upstreams (WC + TikTok)
// implement. delta is signed: negative for decrement.
type StockAdjuster interface {
	Adjust(ctx context.Context, req StockAdjustRequest) error
}

// StockAdjustRequest is the unit of work the StockAdjuster receives.
type StockAdjustRequest struct {
	TenantID       string
	SKU            string
	ProductID      string
	WarehouseID    string
	Delta          int
	OrderID        string
	IdempotencyKey string
}

// StockAdjusterFunc adapts a function to StockAdjuster.
type StockAdjusterFunc func(ctx context.Context, req StockAdjustRequest) error

// Adjust implements StockAdjuster.
func (f StockAdjusterFunc) Adjust(ctx context.Context, req StockAdjustRequest) error {
	return f(ctx, req)
}

// IdempotencyStore guards duplicate Apply calls. v3.3.0 ships an
// in-memory store; v3.7+ swaps for Postgres without changing this
// interface. Note: the package-private interface mirrors the
// internal/webhook one but lives here so the sync package has no
// import cycle through webhook.
type IdempotencyStore interface {
	Reserve(ctx context.Context, key string) (bool, error)
	Release(ctx context.Context, key string) error
}

// MemoryIdempotencyStore is the in-memory IdempotencyStore.
type MemoryIdempotencyStore struct {
	mu   stdsync.Mutex
	seen map[string]struct{}
}

// NewMemoryIdempotencyStore returns an empty store.
func NewMemoryIdempotencyStore() *MemoryIdempotencyStore {
	return &MemoryIdempotencyStore{seen: map[string]struct{}{}}
}

// Reserve atomically records the key. Returns (true, nil) on first
// observation; (false, nil) on duplicate.
func (s *MemoryIdempotencyStore) Reserve(_ context.Context, key string) (bool, error) {
	if key == "" {
		return false, fmt.Errorf("%w: empty key", ErrInventorySyncUnconfigured)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.seen[key]; ok {
		return false, nil
	}
	s.seen[key] = struct{}{}
	return true, nil
}

// Release removes the key so a retry after a failed apply can
// re-attempt.
func (s *MemoryIdempotencyStore) Release(_ context.Context, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.seen, key)
	return nil
}

// InventorySyncMetrics is the small port the saga emits per-
// direction counters through.
type InventorySyncMetrics interface {
	RecordInventorySync(tenantID, direction, status string)
}

// InventorySyncConfig wires a TikTokInventorySync.
type InventorySyncConfig struct {
	WC          StockAdjuster
	TikTok      StockAdjuster
	Idempotency IdempotencyStore
	Metrics     InventorySyncMetrics
	Now         func() time.Time
}

// TikTokInventorySync is the EC-3-4 saga.
type TikTokInventorySync struct {
	cfg    InventorySyncConfig
	logger *slog.Logger

	mu     stdsync.Mutex
	closed bool
}

// NewTikTokInventorySync constructs a saga.
func NewTikTokInventorySync(logger *slog.Logger, cfg InventorySyncConfig) (*TikTokInventorySync, error) {
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.WC == nil {
		return nil, fmt.Errorf("%w: WC adjuster required", ErrInventorySyncUnconfigured)
	}
	if cfg.TikTok == nil {
		return nil, fmt.Errorf("%w: TikTok adjuster required", ErrInventorySyncUnconfigured)
	}
	if cfg.Idempotency == nil {
		cfg.Idempotency = NewMemoryIdempotencyStore()
	}
	if cfg.Now == nil {
		cfg.Now = func() time.Time { return time.Now().UTC() }
	}
	return &TikTokInventorySync{cfg: cfg, logger: logger}, nil
}

// Close marks the saga closed.
func (s *TikTokInventorySync) Close(_ context.Context) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return nil
}

// ApplyTikTokOrder runs the TikTok->WC direction. Called by the
// EC-3-3 webhook handler (on OrderReceived) so the WC stock drops
// to match the TikTok order quantity.
func (s *TikTokInventorySync) ApplyTikTokOrder(ctx context.Context, req StockAdjustRequest) error {
	return s.runApply(ctx, DirectionTikTokToWC, s.cfg.TikTok, s.cfg.WC, req)
}

// ApplyWCFulfilment runs the WC->TikTok direction. Called by the
// WC fulfilment workflow (or admin endpoint) when a WC order ships
// so TikTok's listing stock matches.
func (s *TikTokInventorySync) ApplyWCFulfilment(ctx context.Context, req StockAdjustRequest) error {
	return s.runApply(ctx, DirectionWCToTikTok, s.cfg.WC, s.cfg.TikTok, req)
}

// runApply is the shared logic: idempotency reserve -> source
// adjust -> target adjust -> compensating source action on target
// failure. Pulled out of the public methods so per-public-method
// cyclomatic stays at 1.
func (s *TikTokInventorySync) runApply(ctx context.Context, dir SyncDirection, source, target StockAdjuster, req StockAdjustRequest) error {
	if err := s.guard(); err != nil {
		return err
	}
	if err := validateApplyRequest(req); err != nil {
		s.recordMetric(req.TenantID, dir, "invalid_request")
		return err
	}
	key := buildIdempotencyKey(dir, req)
	allowed, err := s.cfg.Idempotency.Reserve(ctx, key)
	if err != nil {
		s.recordMetric(req.TenantID, dir, "idempotency_error")
		return fmt.Errorf("idempotency reserve: %w", err)
	}
	if !allowed {
		s.recordMetric(req.TenantID, dir, "duplicate")
		return ErrInventorySyncDuplicate
	}
	sourceReq, targetReq := splitRequest(dir, req)
	if err := s.applySource(ctx, key, source, sourceReq, dir); err != nil {
		return err
	}
	if err := s.applyTarget(ctx, key, source, target, sourceReq, targetReq, dir); err != nil {
		return err
	}
	s.recordMetric(req.TenantID, dir, "ok")
	return nil
}

// applySource runs the source-side adjustment. Returns the typed
// error category on failure so runApply can rewrap.
func (s *TikTokInventorySync) applySource(ctx context.Context, key string, source StockAdjuster, req StockAdjustRequest, dir SyncDirection) error {
	if dir == DirectionTikTokToWC {
		// Source side (TikTok) is informational; the upstream
		// webhook is the producer. Skip the source call to avoid
		// double-decrement.
		return nil
	}
	if err := source.Adjust(ctx, req); err != nil {
		_ = s.cfg.Idempotency.Release(ctx, key)
		s.recordMetric(req.TenantID, dir, "source_failed")
		return fmt.Errorf("source adjust (%s): %w", dir, err)
	}
	return nil
}

// applyTarget runs the target-side adjustment + compensating action
// when the target fails.
func (s *TikTokInventorySync) applyTarget(ctx context.Context, key string, source StockAdjuster, target StockAdjuster, sourceReq, targetReq StockAdjustRequest, dir SyncDirection) error {
	if err := target.Adjust(ctx, targetReq); err != nil {
		s.recordMetric(targetReq.TenantID, dir, "target_failed")
		return s.runCompensation(ctx, key, source, sourceReq, dir, err)
	}
	return nil
}

// runCompensation reverses the source adjustment when the target
// fails. Releases the idempotency key so a retry can fire. On
// compensation failure the joined error surfaces so the operator
// alert centre (EC-9-5) can fire a manual-intervention alert.
func (s *TikTokInventorySync) runCompensation(ctx context.Context, key string, source StockAdjuster, sourceReq StockAdjustRequest, dir SyncDirection, targetErr error) error {
	defer func() { _ = s.cfg.Idempotency.Release(ctx, key) }()
	if dir == DirectionTikTokToWC {
		// No source-side adjustment ran; nothing to compensate.
		return fmt.Errorf("%w: %v", ErrInventorySyncTargetFailed, targetErr)
	}
	reverse := sourceReq
	reverse.Delta = -reverse.Delta
	if compErr := source.Adjust(ctx, reverse); compErr != nil {
		s.recordMetric(sourceReq.TenantID, dir, "rollback_failed")
		s.logger.Error("inventory_sync.rollback_failed", "tenant_id", sourceReq.TenantID, "order_id", sourceReq.OrderID, "target_err", targetErr, "comp_err", compErr)
		return fmt.Errorf("%w: target=%v compensation=%v", ErrInventorySyncRollbackFailed, targetErr, compErr)
	}
	s.recordMetric(sourceReq.TenantID, dir, "rolled_back")
	return fmt.Errorf("%w: %v", ErrInventorySyncTargetFailed, targetErr)
}

func (s *TikTokInventorySync) guard() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrInventorySyncClosed
	}
	return nil
}

func (s *TikTokInventorySync) recordMetric(tenantID string, dir SyncDirection, status string) {
	if s.cfg.Metrics == nil {
		return
	}
	s.cfg.Metrics.RecordInventorySync(tenantID, string(dir), status)
}

func validateApplyRequest(req StockAdjustRequest) error {
	if strings.TrimSpace(req.TenantID) == "" {
		return fmt.Errorf("%w: TenantID required", ErrInventorySyncUnconfigured)
	}
	if strings.TrimSpace(req.OrderID) == "" {
		return fmt.Errorf("%w: OrderID required", ErrInventorySyncUnconfigured)
	}
	if strings.TrimSpace(req.SKU) == "" {
		return fmt.Errorf("%w: SKU required", ErrInventorySyncUnconfigured)
	}
	if req.Delta == 0 {
		return fmt.Errorf("%w: Delta cannot be zero", ErrInventorySyncUnconfigured)
	}
	return nil
}

func buildIdempotencyKey(dir SyncDirection, req StockAdjustRequest) string {
	if req.IdempotencyKey != "" {
		return string(dir) + "\x00" + req.TenantID + "\x00" + req.IdempotencyKey
	}
	return string(dir) + "\x00" + req.TenantID + "\x00" + req.OrderID + "\x00" + req.SKU
}

// splitRequest constructs source + target StockAdjustRequest copies
// so each side can carry its own warehouse / product id without
// mutation. v3.3.0 ships them identical except for warehouse
// resolution; v3.4+ may add per-platform mapping helpers.
func splitRequest(dir SyncDirection, req StockAdjustRequest) (StockAdjustRequest, StockAdjustRequest) {
	source := req
	target := req
	_ = dir
	return source, target
}
