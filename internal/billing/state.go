package billing

import "fmt"

// State is the lifecycle state of a per-tenant billing Subscription.
// Mirrors the v2.2.0/v2.3.0/v2.4.0 transition-table pattern so that
// every legal move is encoded once and validated by table-driven
// tests; anything missing is by definition illegal.
type State string

const (
	// StateTrialing is the initial state for a newly-created
	// subscription that is still in its trial window.
	StateTrialing State = "trialing"
	// StateActive is the operating state once the trial converts or
	// when a paused subscription is resumed.
	StateActive State = "active"
	// StatePastDue is reached when an invoice payment fails. Stripe
	// retries; resolution moves back to active or forward to canceled.
	StatePastDue State = "past_due"
	// StatePaused is a reversible disable triggered by tenant or admin.
	StatePaused State = "paused"
	// StateCanceled is terminal: no further transitions out.
	StateCanceled State = "canceled"
)

// Transition is the named action that moves a Subscription from one
// State to another.
type Transition string

const (
	TransitionActivate    Transition = "activate"
	TransitionMarkPastDue Transition = "mark_past_due"
	TransitionRecover     Transition = "recover"
	TransitionPause       Transition = "pause"
	TransitionResume      Transition = "resume"
	TransitionCancel      Transition = "cancel"
)

// transitionTable encodes every legal (from, transition) -> to triple.
// Anything missing here is illegal and produces ErrInvalidTransition.
// `canceled` is terminal.
//
//	trialing  -> active     (activate)         | trial converts
//	trialing  -> canceled   (cancel)           | trial abandoned
//	active    -> past_due   (mark_past_due)    | invoice.payment_failed
//	active    -> paused     (pause)            | tenant or admin pause
//	active    -> canceled   (cancel)           | tenant cancels
//	past_due  -> active     (recover)          | invoice.payment_succeeded
//	past_due  -> canceled   (cancel)           | retries exhausted
//	paused    -> active     (resume)           | resume action
//	paused    -> canceled   (cancel)           | cancel from paused
var transitionTable = map[State]map[Transition]State{
	StateTrialing: {
		TransitionActivate: StateActive,
		TransitionCancel:   StateCanceled,
	},
	StateActive: {
		TransitionMarkPastDue: StatePastDue,
		TransitionPause:       StatePaused,
		TransitionCancel:      StateCanceled,
	},
	StatePastDue: {
		TransitionRecover: StateActive,
		TransitionCancel:  StateCanceled,
	},
	StatePaused: {
		TransitionResume: StateActive,
		TransitionCancel: StateCanceled,
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
	case StateTrialing, StateActive, StatePastDue, StatePaused, StateCanceled:
		return State(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidState, value)
	}
}

// ParseTransition validates and returns the canonical Transition for a
// string value.
func ParseTransition(value string) (Transition, error) {
	switch Transition(value) {
	case TransitionActivate, TransitionMarkPastDue, TransitionRecover,
		TransitionPause, TransitionResume, TransitionCancel:
		return Transition(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidTransitionName, value)
	}
}

// nextState looks up the destination state for a (from, transition)
// pair. Returns ErrInvalidTransition when the move is not in the
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

// AllTransitions returns every legal triple. Used by tests and the
// OpenAPI doc generator to keep the state machine and the spec in
// sync.
func AllTransitions() []TransitionTriple {
	var triples []TransitionTriple
	for from, moves := range transitionTable {
		for t, to := range moves {
			triples = append(triples, TransitionTriple{From: from, Via: t, To: to})
		}
	}
	return triples
}

// ParseInvoiceStatus validates and returns the canonical InvoiceStatus
// for a string value.
func ParseInvoiceStatus(value string) (InvoiceStatus, error) {
	switch InvoiceStatus(value) {
	case InvoiceOpen, InvoicePaid, InvoiceVoid, InvoiceUncollectible:
		return InvoiceStatus(value), nil
	default:
		return "", fmt.Errorf("%w: %q", ErrInvalidInvoiceStatus, value)
	}
}
