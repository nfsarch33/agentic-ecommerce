package rag

import (
	"context"
	"strings"
	"time"
)

type Service struct {
	embedder     Embedder
	store        VectorStore
	chunkOptions ChunkOptions
}

func NewService(embedder Embedder, store VectorStore, chunkOptions ChunkOptions) *Service {
	return &Service{embedder: embedder, store: store, chunkOptions: chunkOptions}
}

func (s *Service) Ingest(ctx context.Context, doc Document) (IngestResult, error) {
	if s.embedder == nil {
		return IngestResult{}, ErrMissingEmbedder
	}
	if s.store == nil {
		return IngestResult{}, ErrMissingStore
	}
	if doc.CreatedAt.IsZero() {
		doc.CreatedAt = time.Now().UTC()
	}
	chunks, err := ChunkDocument(doc, s.chunkOptions)
	if err != nil {
		return IngestResult{}, err
	}
	texts := make([]string, len(chunks))
	for i, chunk := range chunks {
		texts[i] = chunk.Text
	}
	embeddings, err := s.embedder.Embed(ctx, texts)
	if err != nil {
		return IngestResult{}, err
	}
	embedded := make([]EmbeddedChunk, len(chunks))
	for i, chunk := range chunks {
		embedded[i] = EmbeddedChunk{Chunk: chunk, Embedding: embeddings[i]}
	}
	if err := s.store.UpsertChunks(ctx, embedded); err != nil {
		return IngestResult{}, err
	}
	return IngestResult{Document: doc, Chunks: chunks}, nil
}

func (s *Service) SearchText(ctx context.Context, text string, topK int) ([]SearchResult, error) {
	return s.Search(ctx, SearchQuery{Text: text, TopK: topK})
}

func (s *Service) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if s.embedder == nil {
		return nil, ErrMissingEmbedder
	}
	if s.store == nil {
		return nil, ErrMissingStore
	}
	if len(query.Embedding) == 0 {
		text := strings.TrimSpace(query.Text)
		if text == "" {
			return []SearchResult{}, nil
		}
		embeddings, err := s.embedder.Embed(ctx, []string{text})
		if err != nil {
			return nil, err
		}
		if len(embeddings) > 0 {
			query.Embedding = embeddings[0]
		}
	}
	return s.store.Search(ctx, query)
}

var _ EvidenceSearcher = (*Service)(nil)
