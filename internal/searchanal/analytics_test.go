package searchanal_test

import (
	"testing"
	"time"

	"github.com/nfsarch33/helixon-ec/internal/searchanal"
)

func TestQueryLog_RecordAndZeroResults(t *testing.T) {
	t.Parallel()

	log := searchanal.NewQueryLog()
	now := time.Now()

	log.Record(searchanal.QueryEvent{Query: "iphone", ResultCount: 5, SessionID: "s1", OccurredAt: now})
	log.Record(searchanal.QueryEvent{Query: "xyznotfound", ResultCount: 0, SessionID: "s2", OccurredAt: now})
	log.Record(searchanal.QueryEvent{Query: "zeroagain", ResultCount: 0, SessionID: "s3", OccurredAt: now.Add(time.Minute)})
	log.Record(searchanal.QueryEvent{Query: "oldquery", ResultCount: 0, SessionID: "s4", OccurredAt: now.Add(-2 * time.Hour)})

	zeroQ := log.ZeroResultQueries(now.Add(-time.Hour))
	// Only "xyznotfound" and "zeroagain" are within since window with 0 results.
	zeroSet := make(map[string]bool)
	for _, q := range zeroQ {
		zeroSet[q] = true
	}

	if !zeroSet["xyznotfound"] {
		t.Error("expected xyznotfound in zero-result queries")
	}
	if !zeroSet["zeroagain"] {
		t.Error("expected zeroagain in zero-result queries")
	}
	if zeroSet["iphone"] {
		t.Error("iphone has results, should not be in zero-result queries")
	}
	if zeroSet["oldquery"] {
		t.Error("oldquery is before since, should not appear")
	}
}

func TestQueryLog_TopQueries(t *testing.T) {
	t.Parallel()

	log := searchanal.NewQueryLog()
	now := time.Now()

	for i := 0; i < 5; i++ {
		log.Record(searchanal.QueryEvent{Query: "shoes", ResultCount: 10, OccurredAt: now})
	}
	for i := 0; i < 3; i++ {
		log.Record(searchanal.QueryEvent{Query: "boots", ResultCount: 8, OccurredAt: now})
	}
	log.Record(searchanal.QueryEvent{Query: "sandals", ResultCount: 4, OccurredAt: now})

	top := log.TopQueries(2)
	if len(top) != 2 {
		t.Fatalf("expected 2 top queries, got %d", len(top))
	}
	if top[0].Query != "shoes" || top[0].Count != 5 {
		t.Errorf("expected first top query to be 'shoes' with count 5, got %v", top[0])
	}
	if top[1].Query != "boots" || top[1].Count != 3 {
		t.Errorf("expected second top query to be 'boots' with count 3, got %v", top[1])
	}
}

func TestQueryLog_TopQueriesN_LargerThanAvailable(t *testing.T) {
	t.Parallel()

	log := searchanal.NewQueryLog()
	log.Record(searchanal.QueryEvent{Query: "only", ResultCount: 1, OccurredAt: time.Now()})

	top := log.TopQueries(10)
	if len(top) != 1 {
		t.Errorf("expected 1 result when fewer queries than n, got %d", len(top))
	}
}

func TestSynonymStore_AddAndRetrieve(t *testing.T) {
	t.Parallel()

	store := searchanal.NewSynonymStore()
	store.Add("tv", "television")
	store.Add("tv", "display")
	store.Add("tv", "television") // duplicate, should not be added twice

	syns := store.Synonyms("tv")
	if len(syns) != 2 {
		t.Errorf("expected 2 synonyms for tv, got %d: %v", len(syns), syns)
	}

	// Unknown query returns nil.
	if store.Synonyms("unknown") != nil {
		t.Error("expected nil for unknown query")
	}
}

func TestZeroResultHandler_ReturnsSynonyms(t *testing.T) {
	t.Parallel()

	store := searchanal.NewSynonymStore()
	store.Add("iphone", "apple phone")
	store.Add("iphone", "smartphone")

	handler := &searchanal.ZeroResultHandler{}
	suggestions := handler.Suggests("iphone", store)

	if len(suggestions) != 2 {
		t.Fatalf("expected 2 suggestions, got %d", len(suggestions))
	}
	sugSet := make(map[string]bool)
	for _, s := range suggestions {
		sugSet[s] = true
	}
	if !sugSet["apple phone"] || !sugSet["smartphone"] {
		t.Errorf("unexpected suggestions: %v", suggestions)
	}
}

func TestZeroResultHandler_NoSynonyms(t *testing.T) {
	t.Parallel()

	store := searchanal.NewSynonymStore()
	handler := &searchanal.ZeroResultHandler{}
	suggestions := handler.Suggests("xyznotfound", store)
	if suggestions != nil {
		t.Errorf("expected nil suggestions for query with no synonyms, got %v", suggestions)
	}
}
