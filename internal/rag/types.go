package rag

import (
	"context"
	"errors"
	"time"
)

const DefaultEmbeddingDimensions = 1536

var (
	ErrEmptyDocument     = errors.New("rag document content is empty")
	ErrInvalidDimensions = errors.New("embedding dimensions do not match store configuration")
	ErrMissingEmbedder   = errors.New("missing rag embedder")
	ErrMissingStore      = errors.New("missing rag vector store")
)

type Document struct {
	ID        string            `json:"id"`
	TenantID  string            `json:"tenant_id,omitempty"`
	Title     string            `json:"title,omitempty"`
	Source    string            `json:"source,omitempty"`
	Content   string            `json:"content"`
	Metadata  map[string]string `json:"metadata,omitempty"`
	CreatedAt time.Time         `json:"created_at,omitempty"`
}

type Chunk struct {
	ID         string            `json:"id"`
	DocumentID string            `json:"document_id"`
	TenantID   string            `json:"tenant_id,omitempty"`
	Index      int               `json:"index"`
	Title      string            `json:"title,omitempty"`
	Source     string            `json:"source,omitempty"`
	Text       string            `json:"text"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	CreatedAt  time.Time         `json:"created_at,omitempty"`
}

type EmbeddedChunk struct {
	Chunk
	Embedding []float64 `json:"embedding,omitempty"`
}

type SearchQuery struct {
	TenantID  string
	Text      string
	Embedding []float64
	TopK      int
}

type SearchResult struct {
	ChunkID    string            `json:"chunk_id"`
	DocumentID string            `json:"document_id"`
	TenantID   string            `json:"tenant_id,omitempty"`
	Title      string            `json:"title,omitempty"`
	Source     string            `json:"source,omitempty"`
	Text       string            `json:"text"`
	Score      float64           `json:"score"`
	Metadata   map[string]string `json:"metadata,omitempty"`
}

type IngestResult struct {
	Document Document `json:"document"`
	Chunks   []Chunk  `json:"chunks"`
}

type Embedder interface {
	Embed(ctx context.Context, texts []string) ([][]float64, error)
}

type VectorStore interface {
	UpsertChunks(ctx context.Context, chunks []EmbeddedChunk) error
	Search(ctx context.Context, query SearchQuery) ([]SearchResult, error)
}

type EvidenceSearcher interface {
	SearchText(ctx context.Context, query string, topK int) ([]SearchResult, error)
}
