package rag

import (
	"context"
	"math"
	"sort"
	"strings"
	"sync"
)

type InMemoryVectorStore struct {
	mu         sync.RWMutex
	dimensions int
	chunks     map[string]EmbeddedChunk
}

func NewInMemoryVectorStore(dimensions int) *InMemoryVectorStore {
	if dimensions <= 0 {
		dimensions = DefaultEmbeddingDimensions
	}
	return &InMemoryVectorStore{
		dimensions: dimensions,
		chunks:     map[string]EmbeddedChunk{},
	}
}

func (s *InMemoryVectorStore) UpsertChunks(_ context.Context, chunks []EmbeddedChunk) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, chunk := range chunks {
		if err := validateDimensions(chunk.Embedding, s.dimensions); err != nil {
			return err
		}
		chunk.Embedding = append([]float64(nil), chunk.Embedding...)
		chunk.Metadata = copyMetadata(chunk.Metadata)
		s.chunks[chunk.ID] = chunk
	}
	return nil
}

func (s *InMemoryVectorStore) Search(_ context.Context, query SearchQuery) ([]SearchResult, error) {
	if query.TopK <= 0 {
		query.TopK = 5
	}
	if err := validateDimensions(query.Embedding, s.dimensions); err != nil {
		return nil, err
	}
	s.mu.RLock()
	defer s.mu.RUnlock()

	results := make([]SearchResult, 0, len(s.chunks))
	for _, chunk := range s.chunks {
		if query.TenantID != "" && chunk.TenantID != query.TenantID {
			continue
		}
		score := CosineSimilarity(query.Embedding, chunk.Embedding)
		if score <= 0 {
			continue
		}
		results = append(results, SearchResult{
			ChunkID:    chunk.ID,
			DocumentID: chunk.DocumentID,
			TenantID:   chunk.TenantID,
			Title:      chunk.Title,
			Source:     chunk.Source,
			Text:       chunk.Text,
			Score:      score,
			Metadata:   copyMetadata(chunk.Metadata),
		})
	}
	sort.SliceStable(results, func(i, j int) bool {
		if results[i].Score == results[j].Score {
			return strings.Compare(results[i].ChunkID, results[j].ChunkID) < 0
		}
		return results[i].Score > results[j].Score
	})
	if len(results) > query.TopK {
		results = results[:query.TopK]
	}
	return results, nil
}

func CosineSimilarity(a, b []float64) float64 {
	if len(a) == 0 || len(a) != len(b) {
		return 0
	}
	var dot, normA, normB float64
	for i := range a {
		dot += a[i] * b[i]
		normA += a[i] * a[i]
		normB += b[i] * b[i]
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

var _ VectorStore = (*InMemoryVectorStore)(nil)
