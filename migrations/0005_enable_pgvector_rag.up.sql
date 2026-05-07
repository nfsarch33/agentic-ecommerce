CREATE EXTENSION IF NOT EXISTS "uuid-ossp";
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS rag_documents (
    id          UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    tenant_id   TEXT NOT NULL DEFAULT 'default',
    source_type TEXT NOT NULL,
    source_uri  TEXT NOT NULL,
    title       TEXT NOT NULL,
    metadata    JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT rag_documents_source_type_non_empty CHECK (btrim(source_type) <> ''),
    CONSTRAINT rag_documents_source_uri_non_empty CHECK (btrim(source_uri) <> ''),
    CONSTRAINT rag_documents_title_non_empty CHECK (btrim(title) <> ''),
    CONSTRAINT rag_documents_tenant_source_unique UNIQUE (tenant_id, source_uri)
);

CREATE TABLE IF NOT EXISTS rag_document_chunks (
    id                   UUID PRIMARY KEY DEFAULT uuid_generate_v4(),
    document_id          UUID NOT NULL REFERENCES rag_documents(id) ON DELETE CASCADE,
    tenant_id            TEXT NOT NULL DEFAULT 'default',
    chunk_index          INTEGER NOT NULL,
    content              TEXT NOT NULL,
    content_hash         TEXT NOT NULL,
    token_count          INTEGER NOT NULL DEFAULT 0,
    embedding_model      TEXT NOT NULL,
    embedding_dimensions INTEGER NOT NULL DEFAULT 1536,
    embedding            VECTOR(1536) NOT NULL,
    metadata             JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT rag_document_chunks_chunk_index_non_negative CHECK (chunk_index >= 0),
    CONSTRAINT rag_document_chunks_content_non_empty CHECK (btrim(content) <> ''),
    CONSTRAINT rag_document_chunks_content_hash_non_empty CHECK (btrim(content_hash) <> ''),
    CONSTRAINT rag_document_chunks_token_count_non_negative CHECK (token_count >= 0),
    CONSTRAINT rag_document_chunks_embedding_model_non_empty CHECK (btrim(embedding_model) <> ''),
    CONSTRAINT rag_document_chunks_embedding_dimensions_match CHECK (
        embedding_dimensions = 1536
        AND vector_dims(embedding) = embedding_dimensions
    ),
    CONSTRAINT rag_document_chunks_document_chunk_unique UNIQUE (document_id, chunk_index)
);

CREATE INDEX IF NOT EXISTS idx_rag_documents_tenant_source_type ON rag_documents(tenant_id, source_type);
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_document_id ON rag_document_chunks(document_id, chunk_index);
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_tenant_model ON rag_document_chunks(tenant_id, embedding_model);
CREATE INDEX IF NOT EXISTS idx_rag_document_chunks_embedding_cosine ON rag_document_chunks USING hnsw (embedding vector_cosine_ops);
