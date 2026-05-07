package rag

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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
	model      string
}

func NewPGVectorStore(db PgxStore, dimensions int) *PGVectorStore {
	if dimensions <= 0 {
		dimensions = DefaultEmbeddingDimensions
	}
	return &PGVectorStore{db: db, dimensions: dimensions, model: "embo-01"}
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
		sourceURI := chunk.DocumentID
		if strings.TrimSpace(sourceURI) == "" {
			sourceURI = chunk.Source
		}
		_, err = s.db.Exec(ctx, `
			WITH upserted_document AS (
				INSERT INTO rag_documents (tenant_id, source_type, source_uri, title, metadata, created_at, updated_at)
				VALUES ($1, $2, $3, $4, $5, $6, now())
				ON CONFLICT (tenant_id, source_uri) DO UPDATE
				SET source_type = EXCLUDED.source_type,
				    title = EXCLUDED.title,
				    metadata = EXCLUDED.metadata,
				    updated_at = now()
				RETURNING id
			)
			INSERT INTO rag_document_chunks (
				document_id, tenant_id, chunk_index, content, content_hash,
				token_count, embedding_model, embedding_dimensions, embedding, metadata, created_at, updated_at
			)
			SELECT id, $1, $7, $8, $9, $10, $11, $12, $13::vector, $5, $6, now()
			FROM upserted_document
			ON CONFLICT (document_id, chunk_index) DO UPDATE
			SET content = EXCLUDED.content,
			    content_hash = EXCLUDED.content_hash,
			    token_count = EXCLUDED.token_count,
			    embedding_model = EXCLUDED.embedding_model,
			    embedding_dimensions = EXCLUDED.embedding_dimensions,
			    embedding = EXCLUDED.embedding,
			    metadata = EXCLUDED.metadata,
			    updated_at = now()`,
			chunk.TenantID,
			sourceType(chunk.Source),
			sourceURI,
			chunk.Title,
			metadata,
			chunk.CreatedAt,
			chunk.Index,
			chunk.Text,
			contentHash(chunk.Text),
			len(chunkWordRe.FindAllString(chunk.Text, -1)),
			s.model,
			s.dimensions,
			vectorLiteral(chunk.Embedding),
		)
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
		SELECT c.id::text, d.source_uri, c.tenant_id, d.title, d.source_uri, c.content,
		       1 - (c.embedding <=> $1::vector) AS score,
		       c.metadata
		FROM rag_document_chunks c
		JOIN rag_documents d ON d.id = c.document_id
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

func sourceType(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "document"
	}
	return source
}

func contentHash(text string) string {
	sum := sha256.Sum256([]byte(text))
	return hex.EncodeToString(sum[:])
}

var _ VectorStore = (*PGVectorStore)(nil)
