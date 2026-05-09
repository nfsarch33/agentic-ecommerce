package rag

import (
	"context"
	"fmt"
	"testing"

	"github.com/nfsarch33/agentic-ecommerce/internal/workerpool"
)

// BenchmarkTrendIngestor_Run measures the steady-state cost of a
// 3-platform poll + ingest. Used by the v3.2.0 regression bench so
// future sprint workers can compare against this baseline.
func BenchmarkTrendIngestor_Run(b *testing.B) {
	pool := workerpool.New(nil, workerpool.Config{
		Name:       "trend-bench",
		MinWorkers: 4,
		MaxWorkers: 8,
		QueueDepth: 64,
	})
	b.Cleanup(func() { _ = pool.Close(context.Background()) })

	embedder := NewHashEmbedder(DefaultEmbeddingDimensions)
	store := NewInMemoryVectorStore(DefaultEmbeddingDimensions)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 32})
	srcs := []TrendSource{
		&fakeTrendSource{name: "tiktok", records: makeBenchRecords("tt", 10)},
		&fakeTrendSource{name: "google_trends", records: makeBenchRecords("gt", 10)},
		&fakeTrendSource{name: "rednote", records: makeBenchRecords("rn", 10)},
	}
	ingestor, err := NewTrendIngestor(nil, TrendIngestorConfig{
		Sources:  srcs,
		Service:  service,
		Pool:     pool,
		TenantID: "cylrl",
	})
	if err != nil {
		b.Fatalf("NewTrendIngestor: %v", err)
	}
	b.Cleanup(func() { _ = ingestor.Close(context.Background()) })

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := ingestor.Run(context.Background()); err != nil {
			b.Fatalf("Run: %v", err)
		}
	}
}

func makeBenchRecords(prefix string, n int) []TrendRecord {
	out := make([]TrendRecord, n)
	for i := 0; i < n; i++ {
		out[i] = TrendRecord{
			Keyword: fmt.Sprintf("%s-keyword-%d", prefix, i),
			Score:   0.5 + float64(i%5)/10,
			Region:  "AU",
			Volume:  1000 + i*50,
		}
	}
	return out
}
