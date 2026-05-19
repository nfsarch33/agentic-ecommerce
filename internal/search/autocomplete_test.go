package search_test

import (
	"testing"

	"github.com/nfsarch33/helixon-ec/internal/search"
)

func TestAutocomplete_InsertAndPrefixSearch(t *testing.T) {
	t.Parallel()
	tr := search.NewTrie()
	tr.Insert("apple", 10)
	tr.Insert("application", 8)
	tr.Insert("banana", 5)
	results := tr.PrefixSearch("app", 10)
	if len(results) != 2 {
		t.Fatalf("expected 2 results for 'app', got %d", len(results))
	}
}

func TestAutocomplete_PopularityBoostReorders(t *testing.T) {
	t.Parallel()
	suggestions := []search.Suggestion{{Term: "apple", Weight: 10}, {Term: "app", Weight: 5}}
	boosted := search.PopularityBoost(suggestions, []string{"app"})
	if boosted[0].Term != "app" {
		t.Fatalf("expected 'app' first after boost, got %s", boosted[0].Term)
	}
}

func TestAutocomplete_TypoToleranceFindsCloseMatch(t *testing.T) {
	t.Parallel()
	dist := search.EditDistance("appl", "apple")
	if dist != 1 {
		t.Fatalf("expected edit distance 1, got %d", dist)
	}
}

func TestAutocomplete_EmptyPrefixReturnsTop(t *testing.T) {
	t.Parallel()
	tr := search.NewTrie()
	tr.Insert("alpha", 10)
	tr.Insert("beta", 5)
	results := tr.PrefixSearch("", 2)
	if len(results) < 2 {
		t.Fatalf("expected all terms for empty prefix, got %d", len(results))
	}
}

func TestAutocomplete_DeleteRemovesTerm(t *testing.T) {
	t.Parallel()
	tr := search.NewTrie()
	tr.Insert("apple", 10)
	tr.Delete("apple")
	results := tr.PrefixSearch("apple", 10)
	if len(results) != 0 {
		t.Fatalf("expected 0 after delete, got %d", len(results))
	}
}

func TestAutocomplete_LimitTruncatesResults(t *testing.T) {
	t.Parallel()
	tr := search.NewTrie()
	for _, w := range []string{"aa", "ab", "ac", "ad", "ae"} {
		tr.Insert(w, 1)
	}
	results := tr.PrefixSearch("a", 3)
	if len(results) != 3 {
		t.Fatalf("expected 3, got %d", len(results))
	}
}

func TestAutocomplete_NoMatchReturnsEmpty(t *testing.T) {
	t.Parallel()
	tr := search.NewTrie()
	tr.Insert("orange", 5)
	results := tr.PrefixSearch("xyz", 10)
	if len(results) != 0 {
		t.Fatalf("expected empty, got %d", len(results))
	}
}
