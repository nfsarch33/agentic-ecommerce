package marketplace

import "fmt"

// State is the lifecycle state of a per-tenant plugin Installation.
// The state machine mirrors the explicit transition-table pattern used
// by internal/domain/membership/state.go and internal/domain/digital/state.go.
type State string

const (
	// StateInstalled is the initial state after Install. The plugin is
	// known to the registry but not yet receiving events or routing.
	StateInstalled State = "installed"
	// StateActive is the running state. The plugin receives subscribed
	// events and exposed routes are mounted (subject to sandbox rules).
	StateActive State = "active"
	// StateDeactivated is a non-terminal pause. The installation row
	// remains in postgres so settings and history survive a re-activate.
	StateDeactivated State = "deactivated"
	// StateUninstalled is terminal: the installation row is removed
	// and the plugin's routes/event subscriptions are torn down.
	StateUninstalled State = "uninstalled"
)

// Transition is the named action that drives the state machine.
type Transition string

const (
	TransitionActivate   Transition = "activate"
	TransitionDeactivate Transition = "deactivate"
	TransitionUninstall  Transition = "uninstall"
)

// transitionTable encodes every legal (from, transition) -> to triple.
// Anything missing here is by definition illegal and produces
// ErrInvalidTransition. Uninstalled is terminal.
//
// Allowed flow per the v2.4.0 spec:
//
//	installed -> active            (activate)
//	active -> deactivated          (deactivate)
//	deactivated -> active          (activate)
//	deactivated -> uninstalled     (uninstall)
//	installed -> uninstalled       (uninstall, never-activated cleanup)
//	active -> uninstalled          (uninstall, force shut-down)
var transitionTable = map[State]map[Transition]State{
	StateInstalled: {
		TransitionActivate:  StateActive,
		TransitionUninstall: StateUninstalled,
	},
	StateActive: {
		TransitionDeactivate: StateDeactivated,
		TransitionUninstall:  StateUninstalled,
	},
	StateDeactivated: {
		TransitionActivate:  StateActive,
		TransitionUninstall: StateUninstalled,
	},
}

// String returns the canonical string for a State.
func (s State) String() string { return string(s) }

// IsTerminal reports whether the State permits any further transitions.
func (s State) IsTerminal() bool {
	_, ok := transitionTable[s]
	return !ok
}

// ParseState validates and returns the canonical State for a string.
func ParseState(value string) (State, error) {
	switch State(value) {
	case StateInstalled, StateActive, StateDeactivated, StateUninstalled:
		return State(value), nil
	default:
		return "", fmt.Errorf("%w: state=%q", ErrInvalidTransition, value)
	}
}

// ParseTransition validates and returns the canonical Transition for
// a string value.
func ParseTransition(value string) (Transition, error) {
	switch Transition(value) {
	case TransitionActivate, TransitionDeactivate, TransitionUninstall:
		return Transition(value), nil
	default:
		return "", fmt.Errorf("%w: transition=%q", ErrInvalidTransitionName, value)
	}
}

// nextState looks up the destination state for a (from, transition)
// pair. It returns ErrInvalidTransition when the move is not in the
// table.
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

// TransitionTriple documents a legal (from, via, to) move.
type TransitionTriple struct {
	From State
	Via  Transition
	To   State
}

// AllTransitions returns every legal triple for documentation and
// table-driven tests. Keeps the OpenAPI spec and the runtime in sync.
func AllTransitions() []TransitionTriple {
	var triples []TransitionTriple
	for from, moves := range transitionTable {
		for t, to := range moves {
			triples = append(triples, TransitionTriple{From: from, Via: t, To: to})
		}
	}
	return triples
}
