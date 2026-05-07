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
