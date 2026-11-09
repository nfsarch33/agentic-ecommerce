package rollback

import (
	"strings"
	"testing"
)

type restoreCall struct {
	id     string
	fields map[string]string
}

// spyStore records every call; the pure package decides, the spy proves
// which effect the decision chose.
type spyStore struct {
	unpublish []string
	restores  []restoreCall
}

func (s *spyStore) Unpublish(listingID string) error {
	s.unpublish = append(s.unpublish, listingID)
	return nil
}

func (s *spyStore) Restore(listingID string, fields map[string]string) error {
	s.restores = append(s.restores, restoreCall{id: listingID, fields: fields})
	return nil
}

// Scenario 1 (no prior): a published listing that never existed before the
// publish is removed, and the publish log records the reversal.
func TestRollbackRemovesFreshListing(t *testing.T) {
	store := &spyStore{}
	res, err := Run(Listing{ID: "sku-1"}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.unpublish) != 1 || store.unpublish[0] != "sku-1" {
		t.Fatalf("unpublish calls = %v, want exactly [sku-1]", store.unpublish)
	}
	if len(store.restores) != 0 {
		t.Fatalf("a fresh listing must not be restored: %v", store.restores)
	}
	if !res.Changed {
		t.Fatal("the first rollback changes the listing")
	}
	if res.Action != "removed" {
		t.Fatalf("action = %q, want \"removed\"", res.Action)
	}
}

// Scenario 1 (prior existed): the listing is restored to its prior fields.
func TestRollbackRestoresPriorFields(t *testing.T) {
	store := &spyStore{}
	prior := map[string]string{"title": "Old title", "price": "8500"}
	res, err := Run(Listing{ID: "sku-2", Prior: PriorState{Existed: true, Fields: prior}}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.restores) != 1 {
		t.Fatalf("restore calls = %d, want exactly one", len(store.restores))
	}
	got := store.restores[0]
	if got.id != "sku-2" {
		t.Fatalf("restored %q, want sku-2", got.id)
	}
	if got.fields["title"] != "Old title" || got.fields["price"] != "8500" {
		t.Fatalf("restored fields = %v, want the prior fields", got.fields)
	}
	if len(store.unpublish) != 0 {
		t.Fatalf("a listing with a prior state must not be removed: %v", store.unpublish)
	}
	if res.Action != "restored to prior state" {
		t.Fatalf("action = %q, want \"restored to prior state\"", res.Action)
	}
}

// Scenario 1, both branches: the publish log records the reversal - the
// line names the listing and says reversal.
func TestPublishLogRecordsTheReversal(t *testing.T) {
	for _, l := range []Listing{
		{ID: "sku-3"},
		{ID: "sku-4", Prior: PriorState{Existed: true, Fields: map[string]string{"title": "x"}}},
	} {
		res, err := Run(l, &spyStore{})
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if !strings.Contains(res.ReversalLine, l.ID) || !strings.Contains(strings.ToLower(res.ReversalLine), "reversal") {
			t.Fatalf("reversal line = %q, want the listing id and the word reversal", res.ReversalLine)
		}
	}
}

// Scenario 2: a second rollback is a no-op with a note - nothing changes
// and the result says the listing was already rolled back.
func TestSecondRollbackIsNoOpWithNote(t *testing.T) {
	store := &spyStore{}
	res, err := Run(Listing{ID: "sku-5", AlreadyRolledBack: true}, store)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(store.unpublish) != 0 || len(store.restores) != 0 {
		t.Fatalf("a second rollback must touch nothing: %v %v", store.unpublish, store.restores)
	}
	if res.Changed {
		t.Fatal("a second rollback changes nothing")
	}
	if !strings.Contains(res.Note, "sku-5") || !strings.Contains(res.Note, "already rolled back") {
		t.Fatalf("note = %q, want the listing and the words 'already rolled back'", res.Note)
	}
}

// Edge: a listing with no id is an error, and the store is untouched.
func TestEmptyListingIDIsAnError(t *testing.T) {
	store := &spyStore{}
	_, err := Run(Listing{}, store)
	if err == nil {
		t.Fatal("an empty listing id must be an error")
	}
	if len(store.unpublish) != 0 || len(store.restores) != 0 {
		t.Fatalf("an invalid listing must touch nothing: %v %v", store.unpublish, store.restores)
	}
}
