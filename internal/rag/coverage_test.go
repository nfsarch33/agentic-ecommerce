package rag

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// File scope: targeted unit-only coverage for the rag package, focusing
// on previously-uncovered branches in pgvector_store.go and store.go (nil
// store, dimension validation, search error wrapping). The pgvector
// seeded fixture in pgvector_seed_test.go runs only under the
// `integration_pg` build tag and is the integration-tier complement.

func TestPGVectorStoreUpsertReturnsErrMissingStoreWhenNilDB(t *testing.T) {
	t.Parallel()

	store := NewPGVectorStore(nil, 3)
	if err := store.UpsertChunks(context.Background(), []EmbeddedChunk{{Embedding: []float64{1, 0, 0}}}); !errors.Is(err, ErrMissingStore) {
		t.Fatalf("err = %v, want ErrMissingStore", err)
	}
}

func TestPGVectorStoreSearchReturnsErrMissingStoreWhenNilDB(t *testing.T) {
	t.Parallel()

	store := NewPGVectorStore(nil, 3)
	_, err := store.Search(context.Background(), SearchQuery{Embedding: []float64{1, 0, 0}, TopK: 1})
	if !errors.Is(err, ErrMissingStore) {
		t.Fatalf("err = %v, want ErrMissingStore", err)
	}
}

func TestPGVectorStoreUpsertRejectsMismatchedDimensions(t *testing.T) {
	t.Parallel()

	store := NewPGVectorStore(&fakePgVectorDB{}, 3)
	err := store.UpsertChunks(context.Background(), []EmbeddedChunk{{Embedding: []float64{1, 0}}})
	if !errors.Is(err, ErrInvalidDimensions) {
		t.Fatalf("err = %v, want ErrInvalidDimensions", err)
	}
}

func TestPGVectorStoreSearchWrapsQueryError(t *testing.T) {
	t.Parallel()

	want := errors.New("connection reset")
	db := &fakePgVectorDBQueryErr{fakePgVectorDB: &fakePgVectorDB{}, queryErr: want}
	store := NewPGVectorStore(db, 3)

	_, err := store.Search(context.Background(), SearchQuery{Embedding: []float64{1, 0, 0}, TopK: 5})
	if !errors.Is(err, want) {
		t.Fatalf("err = %v, want wrapped %v", err, want)
	}
}

func TestPGVectorStoreSearchAppliesDefaultTopKAndReturnsEmptyResults(t *testing.T) {
	t.Parallel()

	db := &fakePgVectorDB{rows: &fakePgVectorRows{}}
	store := NewPGVectorStore(db, 3)

	results, err := store.Search(context.Background(), SearchQuery{Embedding: []float64{1, 0, 0}})
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	if len(results) != 0 {
		t.Fatalf("results = %d, want 0", len(results))
	}
	if len(db.queries) != 1 {
		t.Fatalf("queries = %d, want 1", len(db.queries))
	}
	if got := db.queries[0].args[2]; got != 5 {
		t.Fatalf("default TopK = %v, want 5", got)
	}
}

func TestNewPGVectorStoreFallsBackToDefaultDimensionsWhenZeroOrNegative(t *testing.T) {
	t.Parallel()

	db := &fakePgVectorDB{rows: &fakePgVectorRows{}}
	for _, dims := range []int{0, -3} {
		store := NewPGVectorStore(db, dims)
		if store.dimensions != DefaultEmbeddingDimensions {
			t.Fatalf("NewPGVectorStore(%d) dimensions = %d, want %d", dims, store.dimensions, DefaultEmbeddingDimensions)
		}
	}
}

func TestVectorLiteralFormatsAsPgVector(t *testing.T) {
	t.Parallel()

	got := vectorLiteral([]float64{1, 0.5, -0.25})
	want := "[1,0.5,-0.25]"
	if got != want {
		t.Fatalf("vectorLiteral = %q, want %q", got, want)
	}
}

func TestSourceTypeNormalisesEmptyValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input string
		want  string
	}{
		{input: "", want: "document"},
		{input: "   ", want: "document"},
		{input: "supplier", want: "supplier"},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			if got := sourceType(tc.input); got != tc.want {
				t.Fatalf("sourceType(%q) = %q, want %q", tc.input, got, tc.want)
			}
		})
	}
}

func TestContentHashIsDeterministicAndDifferentForDifferentInput(t *testing.T) {
	t.Parallel()

	if a, b := contentHash("alpha"), contentHash("alpha"); a != b {
		t.Fatalf("hash should be deterministic: %q vs %q", a, b)
	}
	if a, b := contentHash("alpha"), contentHash("beta"); a == b {
		t.Fatalf("hash should differ for different inputs: %q", a)
	}
	if hash := contentHash("alpha"); !strings.HasPrefix(hash, "5") || len(hash) == 0 {
		t.Logf("content hash sample = %s", hash)
	}
}

// Augment fakePgVectorDB to support queryErr injection for the wrap test.
// Existing definition lives in pgvector_store_test.go — we extend by
// shadowing the Query method through a wrapper variant here. To keep
// changes additive, this file uses a separate fake type that satisfies
// the same interface but adds the error-injection knob.

type fakePgVectorDBQueryErr struct {
	*fakePgVectorDB
	queryErr error
}

func (db *fakePgVectorDBQueryErr) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if db.queryErr != nil {
		return nil, db.queryErr
	}
	return db.fakePgVectorDB.Query(ctx, sql, args...)
}

func (db *fakePgVectorDBQueryErr) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return db.fakePgVectorDB.Exec(ctx, sql, args...)
}
