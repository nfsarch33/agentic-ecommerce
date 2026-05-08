package membership

import (
	"errors"
	"sort"
	"testing"
)

func TestParseState(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    State
		wantErr error
	}{
		{name: "trial", input: "trial", want: StateTrial},
		{name: "active", input: "active", want: StateActive},
		{name: "paused", input: "paused", want: StatePaused},
		{name: "cancelled", input: "cancelled", want: StateCancelled},
		{name: "expired", input: "expired", want: StateExpired},
		{name: "invalid", input: "blue", wantErr: ErrInvalidState},
		{name: "empty", input: "", wantErr: ErrInvalidState},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseState(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("ParseState(%q) err = %v, want %v", tc.input, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ParseState(%q) = %q, %v; want %q", tc.input, got, err, tc.want)
			}
		})
	}
}

func TestParseTransition(t *testing.T) {
	t.Parallel()

	cases := []struct {
		input   string
		want    Transition
		wantErr error
	}{
		{input: "activate", want: TransitionActivate},
		{input: "pause", want: TransitionPause},
		{input: "resume", want: TransitionResume},
		{input: "cancel", want: TransitionCancel},
		{input: "expire", want: TransitionExpire},
		{input: "renew", want: TransitionRenew},
		{input: "explode", wantErr: ErrInvalidTransitionName},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			got, err := ParseTransition(tc.input)
			if tc.wantErr != nil {
				if !errors.Is(err, tc.wantErr) {
					t.Fatalf("err = %v, want %v", err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("got = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestStateIsTerminal(t *testing.T) {
	t.Parallel()

	cases := map[State]bool{
		StateTrial:     false,
		StateActive:    false,
		StatePaused:    false,
		StateCancelled: true,
		StateExpired:   true,
	}
	for state, want := range cases {
		state, want := state, want
		t.Run(string(state), func(t *testing.T) {
			t.Parallel()
			if got := state.IsTerminal(); got != want {
				t.Fatalf("%s.IsTerminal() = %v, want %v", state, got, want)
			}
		})
	}
}

// TestNextStateFullMatrix is the canonical state-machine test: every
// state x every transition must produce either an explicit destination
// state (matching the transition table) or ErrInvalidTransition.
func TestNextStateFullMatrix(t *testing.T) {
	t.Parallel()

	allStates := []State{StateTrial, StateActive, StatePaused, StateCancelled, StateExpired}
	allTransitions := []Transition{
		TransitionActivate, TransitionPause, TransitionResume,
		TransitionCancel, TransitionExpire, TransitionRenew,
	}

	for _, from := range allStates {
		for _, via := range allTransitions {
			from, via := from, via
			t.Run(string(from)+"-"+string(via), func(t *testing.T) {
				t.Parallel()
				got, err := nextState(from, via)
				if expected, ok := transitionTable[from][via]; ok {
					if err != nil || got != expected {
						t.Fatalf("nextState(%s, %s) = %q, %v; want %q", from, via, got, err, expected)
					}
					return
				}
				if !errors.Is(err, ErrInvalidTransition) {
					t.Fatalf("nextState(%s, %s) err = %v, want ErrInvalidTransition", from, via, err)
				}
				if got != "" {
					t.Fatalf("nextState(%s, %s) returned non-empty %q on error", from, via, got)
				}
			})
		}
	}
}

// TestAllTransitionsExportsExpectedTriples validates that AllTransitions
// matches the transition table and is stable enough for OpenAPI doc
// generators to depend on.
func TestAllTransitionsExportsExpectedTriples(t *testing.T) {
	t.Parallel()

	want := map[string]struct{}{
		"trial-activate-active":   {},
		"trial-cancel-cancelled":  {},
		"trial-expire-expired":    {},
		"active-pause-paused":     {},
		"active-renew-active":     {},
		"active-cancel-cancelled": {},
		"active-expire-expired":   {},
		"paused-resume-active":    {},
		"paused-cancel-cancelled": {},
	}
	got := make(map[string]struct{}, len(want))
	for _, triple := range AllTransitions() {
		got[string(triple.From)+"-"+string(triple.Via)+"-"+string(triple.To)] = struct{}{}
	}
	if len(want) != len(got) {
		keys := make([]string, 0, len(got))
		for k := range got {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		t.Fatalf("AllTransitions() len = %d, want %d. got keys = %v", len(got), len(want), keys)
	}
	for k := range want {
		if _, ok := got[k]; !ok {
			t.Errorf("missing transition triple %s", k)
		}
	}
}
