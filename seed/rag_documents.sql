WITH upsert_docs AS (
    INSERT INTO rag_documents (tenant_id, source_type, source_uri, title, metadata)
    VALUES
        (
            'default',
            'fixture',
            'fixture://rag/resistance-band-set',
            'Resistance Band Set Facts',
            '{"fixture": true, "version": "v1.3.0"}'::jsonb
        ),
        (
            'default',
            'fixture',
            'fixture://rag/foam-roller',
            'Foam Roller Facts',
            '{"fixture": true, "version": "v1.3.0"}'::jsonb
        )
    ON CONFLICT (tenant_id, source_uri) DO UPDATE SET
        source_type = EXCLUDED.source_type,
        title = EXCLUDED.title,
        metadata = EXCLUDED.metadata,
        updated_at = now()
    RETURNING id, source_uri
)
INSERT INTO rag_document_chunks (
    document_id,
    tenant_id,
    chunk_index,
    content,
    content_hash,
    token_count,
    embedding_model,
    embedding_dimensions,
    embedding,
    metadata
)
SELECT
    id,
    'default',
    0,
    'Resistance Band Set includes five tension bands, handles, a door anchor, and a carry bag for progressive home workouts.',
    'fixture-rag-resistance-band-set-000',
    17,
    'deterministic-fixture-v1',
    1536,
    array_cat(ARRAY[1.0::real], array_fill(0.0::real, ARRAY[1535]))::vector,
    '{"fixture": true, "keywords": ["resistance", "bands", "home workouts"]}'::jsonb
FROM upsert_docs
WHERE source_uri = 'fixture://rag/resistance-band-set'
UNION ALL
SELECT
    id,
    'default',
    0,
    'Foam Roller has a textured surface for warmups, cooldowns, and muscle recovery after training.',
    'fixture-rag-foam-roller-000',
    14,
    'deterministic-fixture-v1',
    1536,
    array_cat(ARRAY[0.0::real, 1.0::real], array_fill(0.0::real, ARRAY[1534]))::vector,
    '{"fixture": true, "keywords": ["foam roller", "recovery", "training"]}'::jsonb
FROM upsert_docs
WHERE source_uri = 'fixture://rag/foam-roller'
ON CONFLICT (document_id, chunk_index) DO UPDATE SET
    content = EXCLUDED.content,
    content_hash = EXCLUDED.content_hash,
    token_count = EXCLUDED.token_count,
    embedding_model = EXCLUDED.embedding_model,
    embedding_dimensions = EXCLUDED.embedding_dimensions,
    embedding = EXCLUDED.embedding,
    metadata = EXCLUDED.metadata,
    updated_at = now();
