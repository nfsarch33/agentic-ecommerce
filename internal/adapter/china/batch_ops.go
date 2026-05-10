// File scope: v4.6.0 -- Batch operations for 1688/Taobao.
//
// Adds BatchGetProducts for bulk price/stock queries and
// production-scaling config (pool size, timeout tunables).
package china

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"
	"time"
)

// Production scaling env vars.
const (
	EnvChinaAPIPoolSize       = "EC_CHINA_API_POOL_SIZE"
	EnvChinaAPITimeoutSeconds = "EC_CHINA_API_TIMEOUT_SECONDS"
)

// DefaultPoolSize is the default HTTP connection pool size.
const DefaultPoolSize = 10

// DefaultBatchTimeout is the per-batch deadline.
const DefaultBatchTimeout = 30 * time.Second

// ProductionScalingConfig reads pool/timeout from env.
type ProductionScalingConfig struct {
	PoolSize int
	Timeout  time.Duration
}

// LoadProductionScalingConfig reads config from env with defaults.
func LoadProductionScalingConfig() ProductionScalingConfig {
	cfg := ProductionScalingConfig{
		PoolSize: DefaultPoolSize,
		Timeout:  DefaultBatchTimeout,
	}
	if v := os.Getenv(EnvChinaAPIPoolSize); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.PoolSize = n
		}
	}
	if v := os.Getenv(EnvChinaAPITimeoutSeconds); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			cfg.Timeout = time.Duration(n) * time.Second
		}
	}
	return cfg
}

// BatchResult holds one product or an error per SKU.
type BatchResult struct {
	SKU     string
	Product Product
	Err     error
}

// BatchGetProducts fetches multiple products by SKU concurrently,
// bounded by the circuit breaker and a concurrency semaphore.
func BatchGetProducts(ctx context.Context, client Client, cb *CircuitBreaker, skus []string) []BatchResult {
	results := make([]BatchResult, len(skus))
	var wg sync.WaitGroup
	sem := make(chan struct{}, DefaultPoolSize)

	for i, sku := range skus {
		wg.Add(1)
		go func(idx int, s string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			var product Product
			err := cb.Do(ctx, func(c context.Context) error {
				p, e := client.ProductDetail(c, ProductDetailRequest{ExternalID: s})
				if e != nil {
					return e
				}
				product = p
				return nil
			})
			results[idx] = BatchResult{SKU: s, Product: product, Err: err}
		}(i, sku)
	}
	wg.Wait()
	return results
}

// ConnectionPoolStats exposes pool tuning metrics.
type ConnectionPoolStats struct {
	PoolSize         int    `json:"pool_size"`
	TimeoutSeconds   int    `json:"timeout_seconds"`
	CircuitBreakerSt string `json:"circuit_breaker_state"`
	ConsecutiveFails int    `json:"consecutive_failures"`
}

// GetConnectionPoolStats returns current pool + breaker state.
func GetConnectionPoolStats(cfg ProductionScalingConfig, cb *CircuitBreaker) ConnectionPoolStats {
	return ConnectionPoolStats{
		PoolSize:         cfg.PoolSize,
		TimeoutSeconds:   int(cfg.Timeout.Seconds()),
		CircuitBreakerSt: cb.State(),
		ConsecutiveFails: cb.ConsecutiveFailures(),
	}
}

// PoolStatsString returns a human-readable summary.
func (s ConnectionPoolStats) PoolStatsString() string {
	return fmt.Sprintf("pool=%d timeout=%ds breaker=%s fails=%d",
		s.PoolSize, s.TimeoutSeconds, s.CircuitBreakerSt, s.ConsecutiveFails)
}
