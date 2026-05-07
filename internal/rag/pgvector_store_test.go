package rag

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

func TestPGVectorStoreUpsertsDocumentAndChunks(t *testing.T) {
	t.Parallel()

	db := &fakePgVectorDB{}
	store := NewPGVectorStore(db, 3)
	err := store.UpsertChunks(context.Background(), []EmbeddedChunk{{
		Chunk: Chunk{
			ID:         "chunk-1",
			DocumentID: "doc-1",
			TenantID:   "tenant-a",
			Index:      0,
			Title:      "Spec",
			Source:     "supplier",
			Text:       "Resistance Band Set includes five levels.",
			Metadata:   map[string]string{"sku": "RB"},
		},
		Embedding: []float64{1, 0, 0.5},
	}})
	if err != nil {
		t.Fatalf("UpsertChunks: %v", err)
	}
	if len(db.execs) != 1 {
		t.Fatalf("execs = %d, want combined document and chunk upsert", len(db.execs))
	}
	if !strings.Contains(db.execs[0].sql, "rag_document_chunks") {
		t.Fatalf("chunk upsert SQL = %s", db.execs[0].sql)
	}
	if got := db.execs[0].args[12]; got != "[1,0,0.5]" {
		t.Fatalf("embedding literal = %v", got)
	}
}

func TestPGVectorStoreSearchScansResults(t *testing.T) {
	t.Parallel()

	metadata, _ := json.Marshal(map[string]string{"sku": "RB"})
	db := &fakePgVectorDB{rows: &fakePgVectorRows{items: []fakePgVectorRow{{
		chunkID: "chunk-1", documentID: "doc-1", tenantID: "tenant-a", title: "Spec", source: "supplier",
		text: "Resistance Band Set includes five levels.", score: 0.91, metadata: metadata,
	}}}}
	store := NewPGVectorStore(db, 3)

	results, err := store.Search(context.Background(), SearchQuery{TenantID: "tenant-a", Embedding: []float64{1, 0, 0}, TopK: 2})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("results = %d, want 1", len(results))
	}
	if results[0].ChunkID != "chunk-1" || results[0].Metadata["sku"] != "RB" {
		t.Fatalf("result = %+v", results[0])
	}
	if len(db.queries) != 1 || db.queries[0].args[1] != "tenant-a" || db.queries[0].args[2] != 2 {
		t.Fatalf("query calls = %+v", db.queries)
	}
}

func TestPGVectorStoreValidatesDimensions(t *testing.T) {
	t.Parallel()

	store := NewPGVectorStore(&fakePgVectorDB{}, 3)
	_, err := store.Search(context.Background(), SearchQuery{Embedding: []float64{1, 0}, TopK: 1})
	if err == nil {
		t.Fatal("expected dimension validation error")
	}
}

type fakePgVectorExec struct {
	sql  string
	args []any
}

type fakePgVectorQuery struct {
	sql  string
	args []any
}

type fakePgVectorDB struct {
	execs   []fakePgVectorExec
	queries []fakePgVectorQuery
	rows    *fakePgVectorRows
}

func (db *fakePgVectorDB) Exec(_ context.Context, sql string, arguments ...any) (pgconn.CommandTag, error) {
	db.execs = append(db.execs, fakePgVectorExec{sql: sql, args: append([]any(nil), arguments...)})
	return pgconn.NewCommandTag("INSERT 1"), nil
}

func (db *fakePgVectorDB) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	db.queries = append(db.queries, fakePgVectorQuery{sql: sql, args: append([]any(nil), args...)})
	return db.rows, nil
}

type fakePgVectorRow struct {
	chunkID, documentID, tenantID, title, source, text string
	score                                              float64
	metadata                                           []byte
}

type fakePgVectorRows struct {
	items  []fakePgVectorRow
	index  int
	closed bool
}

func (r *fakePgVectorRows) Next() bool {
	if r.index >= len(r.items) {
		return false
	}
	r.index++
	return true
}

func (r *fakePgVectorRows) Scan(dest ...any) error {
	item := r.items[r.index-1]
	*dest[0].(*string) = item.chunkID
	*dest[1].(*string) = item.documentID
	*dest[2].(*string) = item.tenantID
	*dest[3].(*string) = item.title
	*dest[4].(*string) = item.source
	*dest[5].(*string) = item.text
	*dest[6].(*float64) = item.score
	*dest[7].(*[]byte) = item.metadata
	return nil
}

func (r *fakePgVectorRows) Close()                                       { r.closed = true }
func (r *fakePgVectorRows) Err() error                                   { return nil }
func (r *fakePgVectorRows) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (r *fakePgVectorRows) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (r *fakePgVectorRows) RawValues() [][]byte                          { return nil }
func (r *fakePgVectorRows) Values() ([]any, error)                       { return nil, nil }
func (r *fakePgVectorRows) Conn() *pgx.Conn                              { return nil }
