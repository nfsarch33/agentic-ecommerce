// Package rollback reverses one published listing: the rollback removes or
// restores the listing to its prior state, the publish log records the
// reversal, and the rollback is idempotent - running it again changes
// nothing and says so. acceptance/rollback.feature is the contract; the
// strings here are its words, and the listing console's row parser reads
// them.
package rollback

import (
	"errors"
	"fmt"
	"strings"
)

// ErrNoListingID is returned when the listing to roll back has no id.
var ErrNoListingID = errors.New("rollback: listing id is empty")

// PriorState is what the listing was before the publish: a listing that
// never existed is removed; one that existed is restored field by field.
type PriorState struct {
	Existed bool
	Fields  map[string]string
}

// Listing is the published listing to roll back. AlreadyRolledBack is the
// publish log's word on this listing: its reversal entry exists, so a
// further run must change nothing.
type Listing struct {
	ID                string
	Prior             PriorState
	AlreadyRolledBack bool
}

// Store applies the rollback's effect to the marketplace. The package
// decides; the job driver's adapter executes.
type Store interface {
	// Unpublish removes the listing.
	Unpublish(listingID string) error
	// Restore puts the listing back to its prior fields.
	Restore(listingID string, fields map[string]string) error
}

// Result is the rollback's answer: whether anything changed, the action
// taken, the line the publish log records, and - on a second run - the
// note that says the listing was already rolled back.
type Result struct {
	Changed      bool
	Action       string
	ReversalLine string
	Note         string
}

// Run rolls one published listing back: prior existed -> restored to its
// prior fields; no prior -> removed; already rolled back -> nothing
// changes and the note says so. The reversal line names the listing and
// the reversal so the publish log can record it.
func Run(l Listing, store Store) (Result, error) {
	if strings.TrimSpace(l.ID) == "" {
		return Result{}, ErrNoListingID
	}
	if l.AlreadyRolledBack {
		return Result{Changed: false, Note: fmt.Sprintf("%s: already rolled back", l.ID)}, nil
	}
	if l.Prior.Existed {
		if err := store.Restore(l.ID, l.Prior.Fields); err != nil {
			return Result{}, err
		}
		res := Result{Changed: true, Action: "restored to prior state"}
		res.ReversalLine = fmt.Sprintf("%s: reversal recorded, listing %s", l.ID, res.Action)
		return res, nil
	}
	if err := store.Unpublish(l.ID); err != nil {
		return Result{}, err
	}
	res := Result{Changed: true, Action: "removed"}
	res.ReversalLine = fmt.Sprintf("%s: reversal recorded, listing %s", l.ID, res.Action)
	return res, nil
}
