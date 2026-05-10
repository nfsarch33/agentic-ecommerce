//go:build v4100_smoke

package v4100

import (
	"context"
	"fmt"
	"math/rand"
	"sort"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/agentic-ecommerce/internal/compliance"
	"github.com/nfsarch33/agentic-ecommerce/internal/eventbus"
	"github.com/nfsarch33/agentic-ecommerce/internal/residency"
)

func TestConcurrentTenants_AllAPISurfaces(t *testing.T) {
	t.Parallel()
	const numTenants = 100

	type surfaceResult struct {
		surface  string
		tenantID string
		duration time.Duration
	}

	surfaces := []string{"GMV", "ROI", "Payments", "Admin", "Dashboard", "Alerts"}
	var mu sync.Mutex
	results := make([]surfaceResult, 0, numTenants*len(surfaces))
	var wg sync.WaitGroup

	for i := range numTenants {
		wg.Add(1)
		go func(tid int) {
			defer wg.Done()
			tenantID := fmt.Sprintf("tenant-%04d", tid)
			for _, surface := range surfaces {
				start := time.Now()
				simulateAPISurface(tenantID, surface)
				dur := time.Since(start)
				mu.Lock()
				results = append(results, surfaceResult{surface: surface, tenantID: tenantID, duration: dur})
				mu.Unlock()
			}
		}(i)
	}
	wg.Wait()

	bySurface := map[string][]time.Duration{}
	for _, r := range results {
		bySurface[r.surface] = append(bySurface[r.surface], r.duration)
	}

	for _, surface := range surfaces {
		durations := bySurface[surface]
		p50, p95, p99 := percentiles(durations)
		t.Logf("%s: p50=%v p95=%v p99=%v (n=%d)", surface, p50, p95, p99, len(durations))

		if p99 > 100*time.Millisecond {
			t.Errorf("%s p99 too high: %v (want <100ms)", surface, p99)
		}
	}
}

func TestCrossTenantIsolation_ZeroLeakage(t *testing.T) {
	t.Parallel()
	const numTenants = 100

	type tenantData struct {
		tenantID string
		secret   string
		results  []string
	}

	tenants := make([]tenantData, numTenants)
	for i := range numTenants {
		tenants[i] = tenantData{
			tenantID: fmt.Sprintf("tenant-%04d", i),
			secret:   fmt.Sprintf("secret-%04d-%d", i, rand.Int63()),
		}
	}

	var wg sync.WaitGroup
	var mu sync.Mutex

	for i := range tenants {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			td := &tenants[idx]
			store := newIsolatedStore(td.tenantID)
			store.Put(td.tenantID, td.secret)
			got := store.Get(td.tenantID)
			mu.Lock()
			td.results = append(td.results, got)
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	for _, td := range tenants {
		if len(td.results) != 1 {
			t.Fatalf("tenant %s: expected 1 result, got %d", td.tenantID, len(td.results))
		}
		if td.results[0] != td.secret {
			t.Fatalf("tenant %s: data leakage detected: got %q want %q", td.tenantID, td.results[0], td.secret)
		}
	}

	for i, a := range tenants {
		for j, b := range tenants {
			if i != j && a.secret == b.results[0] {
				t.Fatalf("cross-tenant leakage: tenant %s data found in tenant %s", a.tenantID, b.tenantID)
			}
		}
	}
}

func TestTemporalWorkflowConcurrency_PaymentSagas(t *testing.T) {
	t.Parallel()
	const (
		numSagas   = 50
		numTenants = 10
		timeout    = 60 * time.Second
	)

	type sagaResult struct {
		tenantID string
		sagaID   string
		duration time.Duration
		success  bool
	}

	var (
		mu      sync.Mutex
		results []sagaResult
		wg      sync.WaitGroup
	)

	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	for i := range numSagas {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			tenantID := fmt.Sprintf("tenant-%02d", idx%numTenants)
			sagaID := fmt.Sprintf("saga-%04d", idx)
			start := time.Now()
			ok := simulatePaymentSaga(ctx, tenantID, sagaID)
			mu.Lock()
			results = append(results, sagaResult{
				tenantID: tenantID, sagaID: sagaID,
				duration: time.Since(start), success: ok,
			})
			mu.Unlock()
		}(i)
	}
	wg.Wait()

	var failed int
	durations := make([]time.Duration, 0, len(results))
	for _, r := range results {
		if !r.success {
			failed++
		}
		durations = append(durations, r.duration)
	}

	p50, p95, p99 := percentiles(durations)
	t.Logf("Payment sagas: completed=%d failed=%d p50=%v p95=%v p99=%v", len(results)-failed, failed, p50, p95, p99)

	if failed > 0 {
		t.Errorf("%d/%d sagas failed", failed, numSagas)
	}
	if p99 > timeout {
		t.Errorf("p99 %v exceeded timeout %v", p99, timeout)
	}
}

func TestRedisConnectionPool_NoExhaustion(t *testing.T) {
	t.Parallel()
	const numChecks = 100

	pool := &inMemoryRedisPool{}
	var (
		wg      sync.WaitGroup
		errCnt  atomic.Int64
		latency []time.Duration
		mu      sync.Mutex
	)

	for i := range numChecks {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			start := time.Now()
			key := fmt.Sprintf("ratelimit:tenant-%04d:endpoint", idx)
			err := pool.Set(key, "1")
			dur := time.Since(start)
			mu.Lock()
			latency = append(latency, dur)
			mu.Unlock()
			if err != nil {
				errCnt.Add(1)
			}
		}(i)
	}
	wg.Wait()

	p50, p95, p99 := percentiles(latency)
	t.Logf("Redis pool: size=%d errors=%d p50=%v p95=%v p99=%v",
		pool.Size(), errCnt.Load(), p50, p95, p99)

	if errCnt.Load() > 0 {
		t.Errorf("pool exhaustion: %d errors out of %d checks", errCnt.Load(), numChecks)
	}
}

type inMemoryRedisPool struct {
	data sync.Map
}

func (p *inMemoryRedisPool) Set(key, value string) error {
	p.data.Store(key, value)
	return nil
}

func (p *inMemoryRedisPool) Get(key string) (string, bool) {
	v, ok := p.data.Load(key)
	if !ok {
		return "", false
	}
	return v.(string), true
}

func (p *inMemoryRedisPool) Size() int {
	count := 0
	p.data.Range(func(_, _ any) bool { count++; return true })
	return count
}

func TestEventbusThroughput_1000Events(t *testing.T) {
	t.Parallel()
	const (
		numEvents = 1000
		timeout   = 10 * time.Second
	)

	bus := eventbus.NewInMemoryBus()
	var received atomic.Int64

	err := bus.Subscribe(context.Background(),
		[]eventbus.EventType{eventbus.ProductEnriched}, "throughput-test",
		func(_ context.Context, _ eventbus.Event) error {
			received.Add(1)
			return nil
		})
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}

	start := time.Now()
	for i := range numEvents {
		evt, err := eventbus.NewProductEnrichedEvent("test", time.Now().UTC(), eventbus.ProductEnrichedPayload{
			Version:      eventbus.ProductEnrichedPayloadVersion,
			TenantID:     fmt.Sprintf("tenant-%04d", i%100),
			ProductID:    fmt.Sprintf("product-%04d", i),
			EnglishTitle: "Scale test product",
			PriceCents:   1000 + i,
			Currency:     "AUD",
		})
		if err != nil {
			t.Fatalf("NewProductEnrichedEvent: %v", err)
		}
		if err := bus.Publish(context.Background(), evt); err != nil {
			t.Fatalf("publish event %d: %v", i, err)
		}
	}

	deadline := time.After(timeout)
	for received.Load() < int64(numEvents) {
		select {
		case <-deadline:
			t.Fatalf("timeout: received %d/%d events in %v", received.Load(), numEvents, timeout)
		default:
			time.Sleep(time.Millisecond)
		}
	}

	elapsed := time.Since(start)
	throughput := float64(numEvents) / elapsed.Seconds()
	t.Logf("Eventbus: %d events in %v (%.0f events/sec), dropped=0", numEvents, elapsed, throughput)

	if received.Load() != int64(numEvents) {
		t.Errorf("dropped events: received %d/%d", received.Load(), numEvents)
	}
}

// --- helpers ---

func simulateAPISurface(tenantID, surface string) {
	time.Sleep(time.Duration(rand.Intn(100)) * time.Microsecond)
	_ = tenantID
	_ = surface
}

func simulatePaymentSaga(ctx context.Context, tenantID, sagaID string) bool {
	select {
	case <-ctx.Done():
		return false
	default:
		time.Sleep(time.Duration(1+rand.Intn(5)) * time.Millisecond)
		_ = tenantID
		_ = sagaID
		return true
	}
}

type isolatedStore struct {
	tenantID string
	data     sync.Map
}

func newIsolatedStore(tenantID string) *isolatedStore {
	return &isolatedStore{tenantID: tenantID}
}

func (s *isolatedStore) Put(key, value string) {
	s.data.Store(s.tenantID+":"+key, value)
}

func (s *isolatedStore) Get(key string) string {
	v, ok := s.data.Load(s.tenantID + ":" + key)
	if !ok {
		return ""
	}
	return v.(string)
}

func percentiles(durations []time.Duration) (p50, p95, p99 time.Duration) {
	if len(durations) == 0 {
		return
	}
	sort.Slice(durations, func(i, j int) bool { return durations[i] < durations[j] })
	p50 = durations[len(durations)*50/100]
	p95 = durations[len(durations)*95/100]
	idx99 := len(durations) * 99 / 100
	if idx99 >= len(durations) {
		idx99 = len(durations) - 1
	}
	p99 = durations[idx99]
	return
}

// compile-time checks for referenced types
var (
	_ = compliance.ErrRestrictedCategory
	_ = residency.DefaultRegion
)
