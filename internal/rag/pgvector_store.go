package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

type PgxStore interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PGVectorStore struct {
	db         PgxStore
	dimensions int
}

func NewPGVectorStore(db PgxStore, dimensions int) *PGVectorStore {
	if dimensions <= 0 {
		dimensions = DefaultEmbeddingDimensions
	}
	return &PGVectorStore{db: db, dimensions: dimensions}
}

func (s *PGVectorStore) UpsertChunks(ctx context.Context, chunks []EmbeddedChunk) error {
	if s.db == nil {
		return ErrMissingStore
	}
	for _, chunk := range chunks {
		if err := validateDimensions(chunk.Embedding, s.dimensions); err != nil {
			return err
		}
		metadata, err := json.Marshal(chunk.Metadata)
		if err != nil {
			return fmt.Errorf("marshal rag metadata: %w", err)
		}
		_, err = s.db.Exec(ctx, `
			INSERT INTO documents (id, tenant_id, title, source, metadata, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, now())
			ON CONFLICT (id) DO UPDATE
			SET tenant_id = EXCLUDED.tenant_id,
			    title = EXCLUDED.title,
			    source = EXCLUDED.source,
			    metadata = EXCLUDED.metadata,
			    updated_at = now()`,
			chunk.DocumentID, chunk.TenantID, chunk.Title, chunk.Source, metadata, chunk.CreatedAt)
		if err != nil {
			return fmt.Errorf("upsert rag document %s: %w", chunk.DocumentID, err)
		}
		_, err = s.db.Exec(ctx, `
			INSERT INTO document_chunks (id, document_id, tenant_id, chunk_index, content, metadata, embedding, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7::vector, $8, now())
			ON CONFLICT (id) DO UPDATE
			SET tenant_id = EXCLUDED.tenant_id,
			    chunk_index = EXCLUDED.chunk_index,
			    content = EXCLUDED.content,
			    metadata = EXCLUDED.metadata,
			    embedding = EXCLUDED.embedding,
			    updated_at = now()`,
			chunk.ID, chunk.DocumentID, chunk.TenantID, chunk.Index, chunk.Text, metadata, vectorLiteral(chunk.Embedding), chunk.CreatedAt)
		if err != nil {
			return fmt.Errorf("upsert rag chunk %s: %w", chunk.ID, err)
		}
	}
	return nil
}

func (s *PGVectorStore) Search(ctx context.Context, query SearchQuery) ([]SearchResult, error) {
	if s.db == nil {
		return nil, ErrMissingStore
	}
	if query.TopK <= 0 {
		query.TopK = 5
	}
	if err := validateDimensions(query.Embedding, s.dimensions); err != nil {
		return nil, err
	}
	sql := `
		SELECT c.id, c.document_id, c.tenant_id, d.title, d.source, c.content,
		       1 - (c.embedding <=> $1::vector) AS score,
		       c.metadata
		FROM document_chunks c
		JOIN documents d ON d.id = c.document_id
		WHERE ($2 = '' OR c.tenant_id = $2)
		ORDER BY c.embedding <=> $1::vector
		LIMIT $3`
	rows, err := s.db.Query(ctx, sql, vectorLiteral(query.Embedding), query.TenantID, query.TopK)
	if err != nil {
		return nil, fmt.Errorf("search rag chunks: %w", err)
	}
	defer rows.Close()

	results := make([]SearchResult, 0)
	for rows.Next() {
		var result SearchResult
		var metadataRaw []byte
		if err := rows.Scan(&result.ChunkID, &result.DocumentID, &result.TenantID, &result.Title, &result.Source, &result.Text, &result.Score, &metadataRaw); err != nil {
			return nil, fmt.Errorf("scan rag search result: %w", err)
		}
		if len(metadataRaw) > 0 {
			_ = json.Unmarshal(metadataRaw, &result.Metadata)
		}
		results = append(results, result)
	}
	return results, rows.Err()
}

func vectorLiteral(vec []float64) string {
	parts := make([]string, len(vec))
	for i, value := range vec {
		parts[i] = strconv.FormatFloat(value, 'f', -1, 64)
	}
	return "[" + strings.Join(parts, ",") + "]"
}

var _ VectorStore = (*PGVectorStore)(nil)
