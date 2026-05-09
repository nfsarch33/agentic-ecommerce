package rag

import (
	"context"
	"errors"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

func TestExtractTrendStrengthFallbacks(t *testing.T) {
	t.Parallel()

	if got := extractTrendStrength(nil); got != 0.5 {
		t.Fatalf("nil metadata: got %v, want 0.5", got)
	}
	if got := extractTrendStrength(map[string]string{"unrelated": "x"}); got != 0.5 {
		t.Fatalf("missing key: got %v, want 0.5", got)
	}
	if got := extractTrendStrength(map[string]string{"trend_strength": "0.9"}); got <= 0.5 {
		t.Fatalf("strength 0.9: got %v, want > 0.5", got)
	}
	if got := extractTrendStrength(map[string]string{"trend_strength": "garbage"}); got != 0.5 {
		t.Fatalf("garbage strength: got %v, want 0.5 (parse fallback)", got)
	}
	if got := extractTrendStrength(map[string]string{"trend_strength": "1.5"}); got != 1 {
		t.Fatalf("clamped strength: got %v, want 1", got)
	}
}

func TestTrendDocumentIDIsDeterministic(t *testing.T) {
	t.Parallel()

	a := trendDocumentID("cylrl", "tiktok", "Wireless Earbuds 2026")
	b := trendDocumentID("cylrl", "tiktok", "wireless earbuds 2026")
	c := trendDocumentID("cylrl", "tiktok", " WIRELESS EARBUDS 2026 ")
	if a != b || b != c {
		t.Fatalf("trendDocumentID not deterministic across casing/whitespace: %q %q %q", a, b, c)
	}
}

func TestTrendIngestor_TrendScoreEmptyTitleReturnsZero(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 1, MaxWorkers: 1, QueueDepth: 1})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})
	src := &fakeTrendSource{name: "tiktok", records: []TrendRecord{{Keyword: "earbuds", Score: 0.5}}}
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
	if got, _ := ingestor.TrendScore(context.Background(), "cylrl", "", ""); got != 0 {
		t.Fatalf("empty title score: got %v, want 0", got)
	}
}

// TestTrendIngestor_FetchAllCapturesSourceErrors exercises the
// per-source error capture path so a single fake source returning an
// error does not abort the run AND does land in report.Errors.
func TestTrendIngestor_FetchAllCapturesSourceErrors(t *testing.T) {
	t.Parallel()

	pool := workerpool.New(nil, workerpool.Config{Name: "x", MinWorkers: 2, MaxWorkers: 2, QueueDepth: 4})
	t.Cleanup(func() { _ = pool.Close(context.Background()) })
	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})

	good := &fakeTrendSource{name: "tiktok", records: []TrendRecord{{Keyword: "earbuds", Score: 0.5}}}
	bad := &fakeTrendSource{name: "rednote", err: errors.New("rednote down")}

	ingestor, err := NewTrendIngestor(nil, TrendIngestorConfig{
		Sources:  []TrendSource{good, bad},
		Service:  service,
		Pool:     pool,
		TenantID: "cylrl",
	})
	if err != nil {
		t.Fatalf("NewTrendIngestor: %v", err)
	}
	t.Cleanup(func() { _ = ingestor.Close(context.Background()) })

	report, err := ingestor.Run(context.Background())
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if report.RecordsIngested == 0 {
		t.Fatal("good source records should still be ingested")
	}
	if _, ok := report.Errors["rednote"]; !ok {
		t.Fatalf("expected rednote error in report.Errors: %#v", report.Errors)
	}
}
