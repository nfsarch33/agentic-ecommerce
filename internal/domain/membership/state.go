// Package membership models the membership bounded context: subscriptions,
// plans, members, and the explicit subscription lifecycle state machine.
//
// The state machine is data-driven via a transition table so callers never
// scatter `if status == "active"` checks across the codebase; every legal
// move is encoded once and validated by table-driven tests.
package membership

import (
	"errors"
	"fmt"
)

// ErrInvalidState is returned when a string cannot be parsed into a State.
var ErrInvalidState = errors.New("invalid subscription state")

// ErrInvalidTransition is returned when an illegal Transition is requested
// for the current State (e.g. resuming a cancelled subscription).
var ErrInvalidTransition = errors.New("invalid subscription transition")

// ErrInvalidTransitionName is returned when a Transition value is not part
// of the canonical set.
var ErrInvalidTransitionName = errors.New("invalid subscription transition name")

// State is the lifecycle state of a Subscription aggregate.
type State string

const (
	StateTrial     State = "trial"
	StateActive    State = "active"
	StatePaused    State = "paused"
	StateCancelled State = "cancelled"
	StateExpired   State = "expired"
)

// Transition is the named action that moves a Subscription from one State
// to another.
type Transition string

const (
	TransitionActivate Transition = "activate"
	TransitionPause    Transition = "pause"
	TransitionResume   Transition = "resume"
	TransitionCancel   Transition = "cancel"
	TransitionExpire   Transition = "expire"
	TransitionRenew    Transition = "renew"
)

// transitionTable encodes every legal (from, transition) -> to triple.
// Anything missing here is by definition illegal and produces
// ErrInvalidTransition. Cancelled and Expired are terminal.
var transitionTable = map[State]map[Transition]State{
	StateTrial: {
		TransitionActivate: StateActive,
		TransitionCancel:   StateCancelled,
		TransitionExpire:   StateExpired,
	},
	StateActive: {
		TransitionPause:  StatePaused,
		TransitionRenew:  StateActive,
		TransitionCancel: StateCancelled,
		TransitionExpire: StateExpired,
	},
	StatePaused: {
		TransitionResume: StateActive,
		TransitionCancel: StateCancelled,
	},
}

// ParseState validates and returns the canonical State for a string value.
func ParseState(value string) (State, error) {
	switch State(value) {
	case StateTrial, StateActive, StatePaused, StateCancelled, StateExpired:
		return State(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidState, value)
	}
}

// String returns the canonical string for a State.
func (s State) String() string { return string(s) }

// IsTerminal reports whether the State permits any further transitions.
func (s State) IsTerminal() bool {
	_, ok := transitionTable[s]
	return !ok
}

// ParseTransition validates and returns the canonical Transition for a
// string value.
func ParseTransition(value string) (Transition, error) {
	switch Transition(value) {
	case TransitionActivate, TransitionPause, TransitionResume,
		TransitionCancel, TransitionExpire, TransitionRenew:
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

// AllTransitions returns the full (from, transition, to) triples encoded by
// the transition table. Used by tests and the OpenAPI doc generator to keep
// the state machine and the spec in sync.
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
