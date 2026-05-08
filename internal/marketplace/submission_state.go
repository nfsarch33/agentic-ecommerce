package marketplace

import "fmt"

// SubmissionState is the lifecycle state of a vendor-initiated
// plugin submission queued for super-admin review. Mirrors the
// explicit transition-table pattern used by Installation State,
// internal/billing/state.go, and internal/digital state.
type SubmissionState string

const (
	// SubmissionPendingReview is the initial state after a vendor
	// posts /api/v1/marketplace/plugins/submit. The row sits in the
	// queue until a super-admin acts on it.
	SubmissionPendingReview SubmissionState = "pending_review"
	// SubmissionApproved is terminal: super-admin accepted the
	// manifest, the row triggers a publish into marketplace_plugins,
	// and the submitter gets an approval notification.
	SubmissionApproved SubmissionState = "approved"
	// SubmissionRejected is terminal: super-admin declined the
	// manifest. The submitter receives the review_notes payload.
	SubmissionRejected SubmissionState = "rejected"
)

// SubmissionTransition is the named action that drives the state
// machine.
type SubmissionTransition string

const (
	SubmissionTransitionApprove SubmissionTransition = "approve"
	SubmissionTransitionReject  SubmissionTransition = "reject"
)

// submissionTransitionTable encodes every legal triple. Both
// approve and reject are terminal so the table contains a single
// row keyed on pending_review.
var submissionTransitionTable = map[SubmissionState]map[SubmissionTransition]SubmissionState{
	SubmissionPendingReview: {
		SubmissionTransitionApprove: SubmissionApproved,
		SubmissionTransitionReject:  SubmissionRejected,
	},
}

// String returns the canonical string for a SubmissionState.
func (s SubmissionState) String() string { return string(s) }

// IsTerminal reports whether the SubmissionState permits any
// further transitions. Approved and rejected are terminal; all other
// states have a transition row.
func (s SubmissionState) IsTerminal() bool {
	_, ok := submissionTransitionTable[s]
	return !ok
}

// ParseSubmissionState validates and returns the canonical
// SubmissionState for a string.
func ParseSubmissionState(value string) (SubmissionState, error) {
	switch SubmissionState(value) {
	case SubmissionPendingReview, SubmissionApproved, SubmissionRejected:
		return SubmissionState(value), nil
	default:
		return "", fmt.Errorf("%w: state=%q", ErrInvalidTransition, value)
	}
}

// ParseSubmissionTransition validates and returns the canonical
// SubmissionTransition for a string value.
func ParseSubmissionTransition(value string) (SubmissionTransition, error) {
	switch SubmissionTransition(value) {
	case SubmissionTransitionApprove, SubmissionTransitionReject:
		return SubmissionTransition(value), nil
	default:
		return "", fmt.Errorf("%w: transition=%q", ErrInvalidTransitionName, value)
	}
}

// nextSubmissionState looks up the destination state for a
// (from, transition) pair.
func nextSubmissionState(from SubmissionState, t SubmissionTransition) (SubmissionState, error) {
	moves, ok := submissionTransitionTable[from]
	if !ok {
		return "", fmt.Errorf("%w: %s is terminal", ErrInvalidTransition, from)
	}
	to, ok := moves[t]
	if !ok {
		return "", fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, t)
	}
	return to, nil
}

// SubmissionTransitionTriple documents a legal (from, via, to) move.
type SubmissionTransitionTriple struct {
	From SubmissionState
	Via  SubmissionTransition
	To   SubmissionState
}

// AllSubmissionTransitions returns every legal triple for tests and
// docs.
func AllSubmissionTransitions() []SubmissionTransitionTriple {
	var triples []SubmissionTransitionTriple
	for from, moves := range submissionTransitionTable {
		for via, to := range moves {
			triples = append(triples, SubmissionTransitionTriple{From: from, Via: via, To: to})
		}
	}
	return triples
}
