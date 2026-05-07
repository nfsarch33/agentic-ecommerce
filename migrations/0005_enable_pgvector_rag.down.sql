DROP INDEX IF EXISTS idx_rag_document_chunks_embedding_cosine;
DROP INDEX IF EXISTS idx_rag_document_chunks_tenant_model;
DROP INDEX IF EXISTS idx_rag_document_chunks_document_id;
DROP INDEX IF EXISTS idx_rag_documents_tenant_source_type;

DROP TABLE IF EXISTS rag_document_chunks;
DROP TABLE IF EXISTS rag_documents;
