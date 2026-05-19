//go:build chaos

package chaos

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/adapter/china"
	"github.com/nfsarch33/helixon-ec/internal/agent/sourcing"
	"github.com/nfsarch33/helixon-ec/internal/compliance"
	"github.com/nfsarch33/helixon-ec/internal/eventbus"
	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

func TestChinaSourcingChaos_APIFlapStillSelectsHealthyAdapter(t *testing.T) {
	t.Parallel()

	pool := newChaosPool(t)
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })

	agent, err := sourcing.NewChinaSourcingAgent(nil, sourcing.ChinaSourcingConfig{
		Clients: []china.Client{
			&chaosClient{src: china.Source1688, err: errChaosFlap},
			&chaosClient{src: china.SourceTaobao, products: []china.Product{healthyChaosProduct()}},
		},
		Pool:                pool,
		Publisher:           bus,
		OmniParserBridgeURL: "http://bridge.local",
		TenantID:            "tenant-chaos",
		MaxResults:          5,
		Now:                 func() time.Time { return time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewChinaSourcingAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close(context.Background()) })

	result, err := agent.Run(context.Background(), sourcing.SourcingRequest{Keyword: "earbuds", MaxResults: 1})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(result.SelectedProducts) != 1 {
		t.Fatalf("selected = %d, want 1; result=%+v", len(result.SelectedProducts), result)
	}
	if got := result.SelectedProducts[0].Source; got != china.SourceTaobao {
		t.Fatalf("selected source = %q, want taobao", got)
	}
}

func TestChinaSourcingChaos_Taobao429BackoffThenRecovery(t *testing.T) {
	t.Parallel()

	var attempts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		n := attempts.Add(1)
		if n <= 2 {
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"products": []map[string]any{{
				"id":              "tb-chaos-1",
				"title":           "Chaos Recovery Earbuds",
				"native_category": "audio",
				"price_cny":       18.75,
				"moq":             12,
				"lead_time_days":  6,
				"seller_id":       "tb-seller",
				"seller_name":     "Taobao Chaos Seller",
				"seller_rating":   4.8,
				"seller_level":    "gold",
				"monthly_sales":   900,
				"url":             "https://item.taobao.com/item.htm?id=tb-chaos-1",
			}},
		})
	}))
	t.Cleanup(server.Close)

	var sleeps []time.Duration
	client, err := china.NewTaobaoClient(nil, china.ConfigTaobao{
		BaseURL:        server.URL,
		SessionCookie:  "session=test",
		BackoffInitial: 10 * time.Millisecond,
		BackoffMax:     40 * time.Millisecond,
		MaxRetries:     3,
		Sleep: func(d time.Duration) {
			sleeps = append(sleeps, d)
		},
	})
	if err != nil {
		t.Fatalf("NewTaobaoClient: %v", err)
	}
	t.Cleanup(func() { _ = client.Close(context.Background()) })

	products, err := client.Search(context.Background(), china.SearchRequest{Keyword: "earbuds", MaxResults: 1})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if attempts.Load() != 3 {
		t.Fatalf("attempts = %d, want 3", attempts.Load())
	}
	if len(sleeps) != 2 || sleeps[0] != 10*time.Millisecond || sleeps[1] != 20*time.Millisecond {
		t.Fatalf("sleeps = %v, want [10ms 20ms]", sleeps)
	}
	if len(products) != 1 || products[0].ExternalID != "tb-chaos-1" {
		t.Fatalf("products = %+v", products)
	}
}

func TestChinaSourcingChaos_ComplianceNegativesBlockEmission(t *testing.T) {
	t.Parallel()

	pool := newChaosPool(t)
	bus := eventbus.NewInMemoryBus()
	t.Cleanup(func() { _ = bus.Close() })

	agent, err := sourcing.NewChinaSourcingAgent(nil, sourcing.ChinaSourcingConfig{
		Clients: []china.Client{&chaosClient{src: china.Source1688, products: []china.Product{
			healthyChaosProductWithCategory("cn-firearm", "firearms"),
			healthyChaosProductWithCategory("cn-medical", "medical_device"),
		}}},
		Pool:                pool,
		Publisher:           bus,
		OmniParserBridgeURL: "http://bridge.local",
		TenantID:            "tenant-chaos",
		MaxResults:          5,
	})
	if err != nil {
		t.Fatalf("NewChinaSourcingAgent: %v", err)
	}
	t.Cleanup(func() { _ = agent.Close(context.Background()) })

	result, err := agent.Run(context.Background(), sourcing.SourcingRequest{Keyword: "restricted", MaxResults: 2})
	if !errors.Is(err, sourcing.ErrSourcingEmptyResults) {
		t.Fatalf("error = %v, want ErrSourcingEmptyResults", err)
	}
	if len(result.SelectedProducts) != 0 {
		t.Fatalf("selected = %d, want 0", len(result.SelectedProducts))
	}
	if result.RejectedReasons["firearms"] != 1 || result.RejectedReasons["medical_device"] != 1 {
		t.Fatalf("rejected reasons = %v", result.RejectedReasons)
	}
	if got := len(bus.Delivered()); got != 0 {
		t.Fatalf("events delivered = %d, want 0", got)
	}
}

func TestChinaSourcingChaos_ComplianceGateRejectsPlatformAndImportNegatives(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		product  compliance.Product
		blockFor string
	}{
		{
			name:     "au_import_firearms",
			product:  compliance.Product{ID: "p1", TenantID: "tenant-chaos", Category: "firearms", Source: compliance.Source1688},
			blockFor: "all",
		},
		{
			name:     "platform_vape",
			product:  compliance.Product{ID: "p2", TenantID: "tenant-chaos", Category: "vape", Source: compliance.Source1688},
			blockFor: string(compliance.SourceTikTok),
		},
		{
			name:     "subcategory_narcotics",
			product:  compliance.Product{ID: "p3", TenantID: "tenant-chaos", Category: "health", SubCategory: "narcotics", Source: compliance.SourceTaobao},
			blockFor: "all",
		},
	} {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			decision, err := compliance.Evaluate(tc.product)
			if !errors.Is(err, compliance.ErrRestrictedCategory) {
				t.Fatalf("error = %v, want ErrRestrictedCategory", err)
			}
			if decision.Pass {
				t.Fatalf("decision passed: %+v", decision)
			}
			if !containsString(decision.BlockedFor, tc.blockFor) {
				t.Fatalf("BlockedFor = %v, want %q", decision.BlockedFor, tc.blockFor)
			}
		})
	}
}

func BenchmarkSourcingScoreCandidates(b *testing.B) {
	agent := sourcing.NewAgent()
	req := sourcing.Request{Candidates: []sourcing.Candidate{
		{SupplierID: "slow-cheap", SKU: "RB", UnitCostCents: 1200, ShippingCents: 300, EstimatedSellPriceCents: 4995, LeadTimeDays: 18, ReliabilityScore: 0.65, DemandScore: 0.7, CompetitionScore: 0.8},
		{SupplierID: "balanced", SKU: "RB", UnitCostCents: 1500, ShippingCents: 250, EstimatedSellPriceCents: 4995, LeadTimeDays: 7, ReliabilityScore: 0.92, DemandScore: 0.82, CompetitionScore: 0.35},
		{SupplierID: "premium", SKU: "RB", UnitCostCents: 1900, ShippingCents: 180, EstimatedSellPriceCents: 4995, LeadTimeDays: 4, ReliabilityScore: 0.98, DemandScore: 0.85, CompetitionScore: 0.5},
	}}
	for i := 0; i < b.N; i++ {
		if _, err := agent.Score(context.Background(), req); err != nil {
			b.Fatalf("Score: %v", err)
		}
	}
}

var errChaosFlap = errors.New("injected china adapter flap")

type chaosClient struct {
	src      china.Source
	products []china.Product
	err      error
}

func (c *chaosClient) Source() china.Source { return c.src }

func (c *chaosClient) Search(context.Context, china.SearchRequest) ([]china.Product, error) {
	if c.err != nil {
		return nil, c.err
	}
	return append([]china.Product(nil), c.products...), nil
}

func (c *chaosClient) ProductDetail(context.Context, china.ProductDetailRequest) (china.Product, error) {
	return china.Product{}, china.ErrInvalidQuery
}

func (c *chaosClient) Close(context.Context) error { return nil }

func newChaosPool(t *testing.T) *workerpool.Pool {
	t.Helper()
	pool := workerpool.New(nil, workerpool.Config{Name: "china-chaos", MinWorkers: 1, MaxWorkers: 2, QueueDepth: 4})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	return pool
}

func healthyChaosProduct() china.Product {
	return healthyChaosProductWithCategory("tb-chaos-healthy", "electronics")
}

func healthyChaosProductWithCategory(id, category string) china.Product {
	return china.Product{
		ExternalID:       id,
		Source:           china.SourceTaobao,
		Title:            "Chaos Product " + id,
		Category:         category,
		PriceCNYCents:    1800,
		MOQ:              12,
		LeadTimeDays:     6,
		SupplierID:       "supplier-" + id,
		SupplierName:     "Chaos Supplier",
		SupplierRating:   4.8,
		SupplierVerified: true,
		ReviewCount:      200,
		MonthlySalesUnit: 800,
		URL:              "https://example.invalid/" + id,
		FetchedAt:        time.Date(2026, 5, 9, 12, 0, 0, 0, time.UTC),
	}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
