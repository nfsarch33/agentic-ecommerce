package search_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/search"
)

func TestIndexAndRetrieve_Product(t *testing.T) {
	t.Parallel()
	idx := search.NewIndexer()
	idx.IndexProduct(search.Product{ID: "p1", Title: "Resistance Band", Category: "fitness"})
	idx.IndexProduct(search.Product{ID: "p2", Title: "Yoga Mat", Category: "fitness"})

	results := idx.Search("Resistance Band", search.SearchFilter{})
	if len(results) == 0 {
		t.Fatal("expected at least 1 result")
	}
	if results[0].ID != "p1" {
		t.Fatalf("expected p1 first, got %s", results[0].ID)
	}
}

func TestSearch_RelevanceOrdering(t *testing.T) {
	t.Parallel()
	idx := search.NewIndexer()
	idx.IndexProduct(search.Product{ID: "p1", Title: "Blue Running Shoes", Category: "shoes"})
	idx.IndexProduct(search.Product{ID: "p2", Title: "Running Shoes Elite", Category: "shoes"})
	idx.IndexProduct(search.Product{ID: "p3", Title: "Swimming Goggles", Category: "swimming"})

	results := idx.Search("Running Shoes", search.SearchFilter{})
	if len(results) < 2 {
		t.Fatalf("expected at least 2 results, got %d", len(results))
	}
	// shoes results should appear before swimming goggles
	for _, r := range results[:2] {
		if r.Category == "swimming" {
			t.Fatalf("swimming result appeared in top 2 for 'Running Shoes'")
		}
	}
}

func TestSearch_FilterByCategory(t *testing.T) {
	t.Parallel()
	idx := search.NewIndexer()
	idx.IndexProduct(search.Product{ID: "p1", Title: "Protein Bar", Category: "food"})
	idx.IndexProduct(search.Product{ID: "p2", Title: "Energy Bar", Category: "food"})
	idx.IndexProduct(search.Product{ID: "p3", Title: "Resistance Band", Category: "fitness"})

	results := idx.Search("Bar", search.SearchFilter{Category: "food"})
	if len(results) != 2 {
		t.Fatalf("expected 2 food results, got %d", len(results))
	}
	for _, r := range results {
		if r.Category != "food" {
			t.Fatalf("non-food result: %+v", r)
		}
	}
}

func TestSearch_NoMatch_ReturnsEmpty(t *testing.T) {
	t.Parallel()
	idx := search.NewIndexer()
	idx.IndexProduct(search.Product{ID: "p1", Title: "Yoga Mat", Category: "fitness"})

	results := idx.Search("nonexistentterm", search.SearchFilter{})
	if len(results) != 0 {
		t.Fatalf("expected 0 results, got %d", len(results))
	}
}

func TestIndexOrder_Searchable(t *testing.T) {
	t.Parallel()
	idx := search.NewIndexer()
	idx.IndexOrder(search.Order{ID: "o1", CustomerEmail: "alice@example.com", Status: "completed"})

	results := idx.Search("alice", search.SearchFilter{})
	if len(results) == 0 {
		t.Fatal("expected order to be searchable by email")
	}
}

func TestSearch_EmptyIndex(t *testing.T) {
	t.Parallel()
	idx := search.NewIndexer()
	results := idx.Search("anything", search.SearchFilter{})
	if len(results) != 0 {
		t.Fatalf("expected 0 from empty index, got %d", len(results))
	}
}
