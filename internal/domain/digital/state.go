// Package digital models the digital goods bounded context: digital
// products (downloadable artefacts), licences, time-limited download
// tokens, and access grants.
//
// The licence state machine is data-driven via a transition table so
// callers never scatter `if status == "active"` checks across the
// codebase; every legal move is encoded once and validated by
// table-driven tests, mirroring the membership pattern in
// internal/domain/membership/state.go.
package digital

import (
	"errors"
	"fmt"
)

// ErrInvalidState is returned when a string cannot be parsed into a State.
var ErrInvalidState = errors.New("invalid licence state")

// ErrInvalidTransition is returned when an illegal Transition is requested
// for the current State (e.g. revoking a revoked licence).
var ErrInvalidTransition = errors.New("invalid licence transition")

// ErrInvalidTransitionName is returned when a Transition value is not part
// of the canonical set.
var ErrInvalidTransitionName = errors.New("invalid licence transition name")

// State is the lifecycle state of a Licence aggregate.
type State string

const (
	// StateActive is the initial and only non-terminal state. Active
	// licences may be revoked by an admin or expired by the system.
	StateActive State = "active"
	// StateRevoked is a terminal state set by an admin action.
	StateRevoked State = "revoked"
	// StateExpired is a terminal state set by the system when the
	// licence's expiry passes.
	StateExpired State = "expired"
)

// Transition is the named action that moves a Licence from one State
// to another.
type Transition string

const (
	TransitionRevoke Transition = "revoke"
	TransitionExpire Transition = "expire"
)

// transitionTable encodes every legal (from, transition) -> to triple.
// Anything missing here is by definition illegal and produces
// ErrInvalidTransition. Revoked and Expired are terminal.
var transitionTable = map[State]map[Transition]State{
	StateActive: {
		TransitionRevoke: StateRevoked,
		TransitionExpire: StateExpired,
	},
}

// ParseState validates and returns the canonical State for a string value.
func ParseState(value string) (State, error) {
	switch State(value) {
	case StateActive, StateRevoked, StateExpired:
		return State(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidState, value)
	}
}

// String returns the canonical string for a State.
func (s State) String() string { return string(s) }

// String returns the canonical string for a Transition.
func (t Transition) String() string { return string(t) }

// IsTerminal reports whether the State permits any further transitions.
func (s State) IsTerminal() bool {
	_, ok := transitionTable[s]
	return !ok
}

// ParseTransition validates and returns the canonical Transition for a
// string value.
func ParseTransition(value string) (Transition, error) {
	switch Transition(value) {
	case TransitionRevoke, TransitionExpire:
		return Transition(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidTransitionName, value)
	}
}

// nextState looks up the destination state for a (from, transition) pair.
// It returns ErrInvalidTransition when the move is not in the table.
func nextState(from State, t Transition) (State, error) {
	moves, ok := transitionTable[from]
	if !ok {
		return "", fmt.Errorf("%w: %s is terminal", ErrInvalidTransition, from)
	}
	to, ok := moves[t]
	if !ok {
		return "", fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, from, t)
	}
	return to, nil
}

// AllTransitions returns the full (from, transition, to) triples encoded
// by the transition table. Used by tests and the OpenAPI doc generator
// to keep the state machine and the spec in sync.
func AllTransitions() []TransitionTriple {
	var triples []TransitionTriple
	for from, moves := range transitionTable {
		for t, to := range moves {
			triples = append(triples, TransitionTriple{From: from, Via: t, To: to})
		}
	}
	return triples
}

// TransitionTriple documents a legal (from, via, to) move.
type TransitionTriple struct {
	From State
	Via  Transition
	To   State
}
