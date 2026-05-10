package china

import (
	"context"
	"errors"
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
