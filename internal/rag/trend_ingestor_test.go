package rag

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/workerpool"
)

// TestTrendIngestor_StoresKeywordSignalToVector is the RED test for
// v3.2.0 EC-2-4. It exercises the full ingest path: a TrendIngestor
// configured with three TrendSource fakes (TikTok, Google Trends,
// RedNote) runs once, fans out across all three platforms via the
// workerpool, normalizes the keyword-score pairs, and stores them in
// the supplied VectorStore via the rag.Service Ingest path. The
// implementation must:
//
//  1. Persist at least one chunk per platform with the platform name
//     captured in the chunk metadata.
//  2. Tag every chunk with the configured tenant_id.
//  3. Expose the resulting trend score to the EC-1-3 sourcing agent
//     via the TrendSignaler contract -- a higher trending keyword
//     for the platform must yield a higher score for a matching
//     product title.
//  4. Be idempotent on re-run (same source -> same chunk_id).
func TestTrendIngestor_StoresKeywordSignalToVector(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{
		Name:       "trend-test",
		MinWorkers: 2,
		MaxWorkers: 4,
		QueueDepth: 8,
	})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })

	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32, OverlapWords: 0})

	ttSrc := &fakeTrendSource{
		name: "tiktok",
		records: []TrendRecord{
			{Keyword: "wireless earbuds", Score: 0.95, Region: "AU", Volume: 12000},
			{Keyword: "kitchen gadget", Score: 0.40, Region: "AU", Volume: 5000},
		},
	}
	gtSrc := &fakeTrendSource{
		name: "google_trends",
		records: []TrendRecord{
			{Keyword: "earbuds", Score: 0.75, Region: "AU", Volume: 9000},
		},
	}
	rnSrc := &fakeTrendSource{
		name: "rednote",
		records: []TrendRecord{
			{Keyword: "好物推荐 earbuds", Score: 0.88, Region: "CN", Volume: 7000},
		},
	}

	ingestor, err := NewTrendIngestor(nil, TrendIngestorConfig{
		Sources:  []TrendSource{ttSrc, gtSrc, rnSrc},
		Service:  service,
		Pool:     pool,
		TenantID: "cylrl",
		Now:      func() time.Time { return time.Date(2026, 5, 9, 0, 0, 0, 0, time.UTC) },
	})
	if err != nil {
		t.Fatalf("NewTrendIngestor: %v", err)
	}
	t.Cleanup(func() { _ = ingestor.Close(context.Background()) })

	report, err := ingestor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.RecordsIngested < 4 {
		t.Fatalf("ingested %d records, want >= 4", report.RecordsIngested)
	}
	if report.PlatformCounts["tiktok"] == 0 {
		t.Fatalf("expected tiktok platform count > 0; got %#v", report.PlatformCounts)
	}
	if report.PlatformCounts["google_trends"] == 0 {
		t.Fatalf("expected google_trends platform count > 0; got %#v", report.PlatformCounts)
	}
	if report.PlatformCounts["rednote"] == 0 {
		t.Fatalf("expected rednote platform count > 0; got %#v", report.PlatformCounts)
	}

	// Idempotent re-run: same chunks, same store size.
	sizeBefore := store.Size()
	if _, err := ingestor.Run(context.Background()); err != nil {
		t.Fatalf("Run (idempotent): %v", err)
	}
	if got := store.Size(); got != sizeBefore {
		t.Fatalf("idempotent re-run grew store from %d to %d", sizeBefore, got)
	}

	// Trend score lookup: a product title matching a strong trending
	// keyword should score higher than one matching no trends.
	trendingScore, err := ingestor.TrendScore(context.Background(), "cylrl", "earbuds", "Premium Wireless Earbuds")
	if err != nil {
		t.Fatalf("TrendScore (trending): %v", err)
	}
	noiseScore, _ := ingestor.TrendScore(context.Background(), "cylrl", "earbuds", "Industrial Lathe Bearing")
	if trendingScore <= noiseScore {
		t.Fatalf("trending score %.4f <= noise score %.4f; trend signal not biasing ranking", trendingScore, noiseScore)
	}
}

func TestNewTrendIngestor_RejectsMissingDependencies(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})
	src := &fakeTrendSource{name: "x"}

	cases := []struct {
		name string
		mut  func(c *TrendIngestorConfig)
	}{
		{name: "no sources", mut: func(c *TrendIngestorConfig) { c.Sources = nil }},
		{name: "no service", mut: func(c *TrendIngestorConfig) { c.Service = nil }},
		{name: "no pool", mut: func(c *TrendIngestorConfig) { c.Pool = nil }},
		{name: "no tenant", mut: func(c *TrendIngestorConfig) { c.TenantID = " " }},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := TrendIngestorConfig{
				Sources:  []TrendSource{src},
				Service:  service,
				Pool:     pool,
				TenantID: "cylrl",
			}
			tc.mut(&cfg)
			_, err := NewTrendIngestor(nil, cfg)
			if err == nil {
				t.Fatal("expected error")
			}
			if !errors.Is(err, ErrTrendIngestorUnconfigured) {
				t.Fatalf("error not wrapping ErrTrendIngestorUnconfigured: %v", err)
			}
		})
	}
}

func TestTrendIngestor_StaleDataReturnsErrTrendStaleData(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})

	// All sources return zero records -> stale data.
	src := &fakeTrendSource{name: "tiktok", records: nil}
	ingestor, err := NewTrendIngestor(nil, TrendIngestorConfig{
		Sources:  []TrendSource{src},
		Service:  service,
		Pool:     pool,
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewTrendIngestor: %v", err)
	}
	t.Cleanup(func() { _ = ingestor.Close(context.Background()) })

	_, err = ingestor.Run(context.Background())
	if !errors.Is(err, ErrTrendStaleData) {
		t.Fatalf("error = %v, want ErrTrendStaleData", err)
	}
}

func TestTrendIngestor_TrendScoreReturnsZeroWhenNoMatch(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})

	src := &fakeTrendSource{
		name:    "tiktok",
		records: []TrendRecord{{Keyword: "kitchen", Score: 0.5, Region: "AU"}},
	}
	ingestor, err := NewTrendIngestor(nil, TrendIngestorConfig{
		Sources:  []TrendSource{src},
		Service:  service,
		Pool:     pool,
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewTrendIngestor: %v", err)
	}
	t.Cleanup(func() { _ = ingestor.Close(context.Background()) })

	if _, err := ingestor.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Different tenant -> no signal.
	score, err := ingestor.TrendScore(context.Background(), "other-tenant", "kitchen", "Kitchen Pot")
	if err != nil {
		t.Fatalf("TrendScore: %v", err)
	}
	if score != 0 {
		t.Fatalf("score = %v, want 0", score)
	}
}

// TestTrendIngestor_RunAfterCloseFails verifies the lifecycle.Closer
// contract: once Close has been called, Run rejects with
// ErrTrendIngestorClosed.
func TestTrendIngestor_RunAfterCloseFails(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})
	src := &fakeTrendSource{name: "tiktok", records: []TrendRecord{{Keyword: "x", Score: 0.5}}}

	ingestor, err := NewTrendIngestor(nil, TrendIngestorConfig{
		Sources:  []TrendSource{src},
		Service:  service,
		Pool:     pool,
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewTrendIngestor: %v", err)
	}
	if err := ingestor.Close(context.Background()); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := ingestor.Run(context.Background()); !errors.Is(err, ErrTrendIngestorClosed) {
		t.Fatalf("Run after Close: error = %v, want ErrTrendIngestorClosed", err)
	}
}

// fakeTrendSource is a deterministic in-memory TrendSource used in
// the EC-2-4 unit tests. Records are returned verbatim; the platform
// name is taken from the constructor.
type fakeTrendSource struct {
	name    string
	records []TrendRecord
	err     error
}

func (f *fakeTrendSource) Platform() string { return f.name }

func (f *fakeTrendSource) Fetch(_ context.Context) ([]TrendRecord, error) {
	if f.err != nil {
		return nil, f.err
	}
	out := make([]TrendRecord, len(f.records))
	copy(out, f.records)
	return out, nil
}
