package china

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type mockChinaClient struct {
	products map[string]Product
	failSKUs map[string]bool
}

func (m *mockChinaClient) Source() Source                { return Source1688 }
func (m *mockChinaClient) Close(_ context.Context) error { return nil }
func (m *mockChinaClient) Search(_ context.Context, _ SearchRequest) ([]Product, error) {
	return nil, nil
}
func (m *mockChinaClient) ProductDetail(_ context.Context, req ProductDetailRequest) (Product, error) {
	if m.failSKUs[req.ExternalID] {
		return Product{}, errors.New("api error")
	}
	if p, ok := m.products[req.ExternalID]; ok {
		return p, nil
	}
	return Product{}, errors.New("not found")
}

func TestBatchGetProducts_Success(t *testing.T) {
	t.Parallel()
	client := &mockChinaClient{
		products: map[string]Product{
			"sku-1": {ExternalID: "sku-1", Title: "Product 1", PriceCNYCents: 1000},
			"sku-2": {ExternalID: "sku-2", Title: "Product 2", PriceCNYCents: 2000},
			"sku-3": {ExternalID: "sku-3", Title: "Product 3", PriceCNYCents: 3000},
		},
	}
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 5})
	results := BatchGetProducts(context.Background(), client, cb, []string{"sku-1", "sku-2", "sku-3"})

	if len(results) != 3 {
		t.Fatalf("results=%d want=3", len(results))
	}
	for _, r := range results {
		if r.Err != nil {
			t.Fatalf("unexpected error for %s: %v", r.SKU, r.Err)
		}
	}
}

func TestBatchGetProducts_CircuitBreakerTrips(t *testing.T) {
	t.Parallel()
	client := &mockChinaClient{failSKUs: map[string]bool{
		"f1": true, "f2": true, "f3": true, "f4": true, "f5": true,
	}}
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 5})

	for _, sku := range []string{"f1", "f2", "f3", "f4", "f5"} {
		_ = cb.Do(context.Background(), func(ctx context.Context) error {
			_, err := client.ProductDetail(ctx, ProductDetailRequest{ExternalID: sku})
			return err
		})
	}
	if cb.State() != StateOpen {
		t.Fatalf("expected open, got %s", cb.State())
	}

	results := BatchGetProducts(context.Background(), client, cb, []string{"sku-6"})
	if len(results) != 1 || !errors.Is(results[0].Err, ErrCircuitOpen) {
		t.Fatalf("expected ErrCircuitOpen for blocked batch, got: %v", results[0].Err)
	}
}

func TestBatchGetProducts_BoundsWorkerGoroutines(t *testing.T) {
	release := make(chan struct{})
	client := &blockingChinaClient{
		started: make(chan string, DefaultPoolSize*2),
		release: release,
	}
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 100})

	skus := make([]string, DefaultPoolSize*8)
	for i := range skus {
		skus[i] = fmt.Sprintf("sku-%02d", i)
	}

	before := runtime.NumGoroutine()
	done := make(chan []BatchResult, 1)
	go func() {
		done <- BatchGetProducts(context.Background(), client, cb, skus)
	}()

	for i := 0; i < DefaultPoolSize; i++ {
		select {
		case <-client.started:
		case <-time.After(time.Second):
			t.Fatalf("started workers=%d want at least %d", i, DefaultPoolSize)
		}
	}

	time.Sleep(50 * time.Millisecond)
	if delta := runtime.NumGoroutine() - before; delta > DefaultPoolSize+8 {
		close(release)
		t.Fatalf("goroutine delta=%d want <=%d for bounded worker queue", delta, DefaultPoolSize+8)
	}

	close(release)
	select {
	case results := <-done:
		if len(results) != len(skus) {
			t.Fatalf("results=%d want=%d", len(results), len(skus))
		}
	case <-time.After(2 * time.Second):
		t.Fatal("BatchGetProducts did not drain after release")
	}
	if got := client.maxInflight.Load(); got > int64(DefaultPoolSize) {
		t.Fatalf("max inflight=%d want <=%d", got, DefaultPoolSize)
	}
}

func TestConnectionPoolStats(t *testing.T) {
	t.Parallel()
	cb := NewCircuitBreaker(CircuitBreakerConfig{FailureThreshold: 5})
	cfg := ProductionScalingConfig{PoolSize: 20, Timeout: 45 * time.Second}
	stats := GetConnectionPoolStats(cfg, cb)

	if stats.PoolSize != 20 {
		t.Fatalf("PoolSize=%d want=20", stats.PoolSize)
	}
	if stats.CircuitBreakerSt != StateClosed {
		t.Fatalf("state=%s want=closed", stats.CircuitBreakerSt)
	}
	s := stats.PoolStatsString()
	if s == "" {
		t.Fatal("empty stats string")
	}
}

type blockingChinaClient struct {
	started     chan string
	release     <-chan struct{}
	inflight    atomic.Int64
	maxInflight atomic.Int64
}

func (b *blockingChinaClient) Source() Source                { return Source1688 }
func (b *blockingChinaClient) Close(_ context.Context) error { return nil }
func (b *blockingChinaClient) Search(_ context.Context, _ SearchRequest) ([]Product, error) {
	return nil, nil
}
func (b *blockingChinaClient) ProductDetail(ctx context.Context, req ProductDetailRequest) (Product, error) {
	current := b.inflight.Add(1)
	for {
		max := b.maxInflight.Load()
		if current <= max || b.maxInflight.CompareAndSwap(max, current) {
			break
		}
	}
	select {
	case b.started <- req.ExternalID:
	default:
	}
	defer b.inflight.Add(-1)

	select {
	case <-ctx.Done():
		return Product{}, ctx.Err()
	case <-b.release:
		return Product{ExternalID: req.ExternalID, Title: req.ExternalID}, nil
	}
}
