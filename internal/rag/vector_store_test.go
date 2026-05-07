package rag

import (
	"context"
	"testing"
)

func TestInMemoryVectorStoreSearchOrdersByCosineSimilarity(t *testing.T) {
	t.Parallel()

	store := NewInMemoryVectorStore(3)
	ctx := context.Background()
	err := store.UpsertChunks(ctx, []EmbeddedChunk{
		{
			Chunk:     Chunk{ID: "chunk-a", DocumentID: "doc-a", Text: "latex resistance band set", Source: "spec-a"},
			Embedding: []float64{1, 0, 0},
		},
		{
			Chunk:     Chunk{ID: "chunk-b", DocumentID: "doc-b", Text: "ceramic coffee mug", Source: "spec-b"},
			Embedding: []float64{0, 1, 0},
		},
		{
			Chunk:     Chunk{ID: "chunk-c", DocumentID: "doc-c", Text: "foam recovery roller", Source: "spec-c"},
			Embedding: []float64{0.7, 0, 0.7},
		},
	})
	if err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	results, err := store.Search(ctx, SearchQuery{Embedding: []float64{1, 0, 0}, TopK: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("results = %d, want 2", len(results))
	}
	if results[0].ChunkID != "chunk-a" {
		t.Fatalf("top result = %+v, want chunk-a", results[0])
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("scores not descending: %+v", results)
	}
}

func TestVectorSearchServiceEmbedsQueriesAndDocuments(t *testing.T) {
	t.Parallel()

	embedder := NewHashEmbedder(16)
	store := NewInMemoryVectorStore(16)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 20})
	ctx := context.Background()

	doc := Document{
		ID:      "doc-rb",
		Title:   "Resistance Band Spec",
		Source:  "supplier-spec",
		Content: "Resistance Band Set includes five resistance levels and natural latex construction.",
	}
	ingested, err := service.Ingest(ctx, doc)
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(ingested.Chunks) != 1 {
		t.Fatalf("ingested chunks = %d, want 1", len(ingested.Chunks))
	}

	results, err := service.SearchText(ctx, "Does the set include five resistance levels?", 3)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if len(results) == 0 {
		t.Fatal("expected at least one evidence result")
	}
	if results[0].DocumentID != "doc-rb" || results[0].Text == "" {
		t.Fatalf("top result = %+v", results[0])
	}
}

func TestInMemoryVectorStoreRejectsWrongDimensions(t *testing.T) {
	t.Parallel()

	store := NewInMemoryVectorStore(3)
	err := store.UpsertChunks(context.Background(), []EmbeddedChunk{{
		Chunk:     Chunk{ID: "bad"},
		Embedding: []float64{1, 2},
	}})
	if err == nil {
		t.Fatal("expected dimension error")
	}
}

func TestVectorSearchServiceRanksWithDeterministicFakeEmbeddings(t *testing.T) {
	t.Parallel()

	embedder := fakeEmbeddingProvider{vectors: map[string][]float64{
		"Resistance Band Set includes five resistance levels natural latex": {1, 0, 0},
		"Ceramic mug holds 350 ml and is dishwasher safe":                   {0, 1, 0},
		"Foam roller uses EVA foam for recovery sessions":                   {0.5, 0, 0.5},
		"five resistance levels":                                            {1, 0, 0},
	}}
	store := NewInMemoryVectorStore(3)
	service := NewService(embedder, store, ChunkOptions{MaxWords: 20})
	ctx := context.Background()

	for _, doc := range []Document{
		{ID: "doc-rb", Content: "Resistance Band Set includes five resistance levels natural latex"},
		{ID: "doc-mug", Content: "Ceramic mug holds 350 ml and is dishwasher safe"},
		{ID: "doc-roller", Content: "Foam roller uses EVA foam for recovery sessions"},
	} {
		if _, err := service.Ingest(ctx, doc); err != nil {
			t.Fatalf("Ingest(%s): %v", doc.ID, err)
		}
	}

	results, err := service.SearchText(ctx, "five resistance levels", 3)
	if err != nil {
		t.Fatalf("SearchText: %v", err)
	}
	if got := resultDocumentIDs(results); len(got) != 2 || got[0] != "doc-rb" || got[1] != "doc-roller" {
		t.Fatalf("ranked docs = %+v; results=%+v", got, results)
	}
	if results[0].Score <= results[1].Score {
		t.Fatalf("scores should descend: %+v", results)
	}
}

func TestInMemoryVectorStoreFiltersTenantAndNonPositiveMatches(t *testing.T) {
	t.Parallel()

	store := NewInMemoryVectorStore(2)
	ctx := context.Background()
	err := store.UpsertChunks(ctx, []EmbeddedChunk{
		{Chunk: Chunk{ID: "tenant-a-hit", DocumentID: "doc-a", TenantID: "tenant-a", Text: "hit"}, Embedding: []float64{1, 0}},
		{Chunk: Chunk{ID: "tenant-b-hit", DocumentID: "doc-b", TenantID: "tenant-b", Text: "other tenant"}, Embedding: []float64{1, 0}},
		{Chunk: Chunk{ID: "tenant-a-zero", DocumentID: "doc-zero", TenantID: "tenant-a", Text: "orthogonal"}, Embedding: []float64{0, 1}},
	})
	if err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}

	results, err := store.Search(ctx, SearchQuery{TenantID: "tenant-a", Embedding: []float64{1, 0}, TopK: 10})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 || results[0].ChunkID != "tenant-a-hit" {
		t.Fatalf("tenant/threshold filtered results = %+v", results)
	}
}

type fakeEmbeddingProvider struct {
	vectors map[string][]float64
}

func (f fakeEmbeddingProvider) Embed(_ context.Context, texts []string) ([][]float64, error) {
	out := make([][]float64, len(texts))
	for i, text := range texts {
		out[i] = append([]float64(nil), f.vectors[text]...)
	}
	return out, nil
}

func resultDocumentIDs(results []SearchResult) []string {
	out := make([]string, len(results))
	for i, result := range results {
		out[i] = result.DocumentID
	}
	return out
}
