DO $$
DECLARE
    top_source_uri TEXT;
BEGIN
    WITH query_embedding AS (
        SELECT array_cat(ARRAY[1.0::real], array_fill(0.0::real, ARRAY[1535]))::vector AS embedding
    )
    SELECT d.source_uri
    INTO top_source_uri
    FROM rag_document_chunks c
    JOIN rag_documents d ON d.id = c.document_id
    CROSS JOIN query_embedding q
    WHERE c.tenant_id = 'default'
      AND c.embedding_model = 'deterministic-fixture-v1'
    ORDER BY c.embedding <=> q.embedding
    LIMIT 1;

    IF top_source_uri IS DISTINCT FROM 'fixture://rag/resistance-band-set' THEN
        RAISE EXCEPTION 'unexpected top RAG result: %', COALESCE(top_source_uri, '<none>');
    END IF;
END $$;

WITH query_embedding AS (
    SELECT array_cat(ARRAY[1.0::real], array_fill(0.0::real, ARRAY[1535]))::vector AS embedding
)
SELECT
    d.source_uri,
    c.chunk_index,
    round((c.embedding <=> q.embedding)::numeric, 6) AS cosine_distance,
    c.content
FROM rag_document_chunks c
JOIN rag_documents d ON d.id = c.document_id
CROSS JOIN query_embedding q
WHERE c.tenant_id = 'default'
  AND c.embedding_model = 'deterministic-fixture-v1'
ORDER BY c.embedding <=> q.embedding
LIMIT :rag_limit;
