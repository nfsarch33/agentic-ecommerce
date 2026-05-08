package digital

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
		{name: "active", input: "active", want: StateActive},
		{name: "revoked", input: "revoked", want: StateRevoked},
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
		{input: "revoke", want: TransitionRevoke},
		{input: "expire", want: TransitionExpire},
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
	cases := []struct {
		state State
		want  bool
	}{
		{state: StateActive, want: false},
		{state: StateRevoked, want: true},
		{state: StateExpired, want: true},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.state.String(), func(t *testing.T) {
			t.Parallel()
			if got := tc.state.IsTerminal(); got != tc.want {
				t.Fatalf("IsTerminal(%q) = %v, want %v", tc.state, got, tc.want)
			}
		})
	}
}

// TestNextStateLegalTransitions exercises every legal (from, transition)
// triple encoded by the transition table.
func TestNextStateLegalTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from State
		via  Transition
		to   State
	}{
		{StateActive, TransitionRevoke, StateRevoked},
		{StateActive, TransitionExpire, StateExpired},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.from.String()+"->"+tc.via.String(), func(t *testing.T) {
			t.Parallel()
			got, err := nextState(tc.from, tc.via)
			if err != nil {
				t.Fatalf("nextState(%q, %q) err = %v", tc.from, tc.via, err)
			}
			if got != tc.to {
				t.Fatalf("nextState(%q, %q) = %q, want %q", tc.from, tc.via, got, tc.to)
			}
		})
	}
}

// TestNextStateIllegalTransitions exercises every (from, transition)
// pair NOT encoded by the table. Each must return ErrInvalidTransition.
func TestNextStateIllegalTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		from State
		via  Transition
	}{
		// Active has no other transitions beyond revoke/expire (already
		// covered as legal). Add unknown verbs as guard.
		{StateRevoked, TransitionRevoke},
		{StateRevoked, TransitionExpire},
		{StateExpired, TransitionRevoke},
		{StateExpired, TransitionExpire},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.from.String()+"->"+tc.via.String(), func(t *testing.T) {
			t.Parallel()
			_, err := nextState(tc.from, tc.via)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("nextState(%q, %q) err = %v, want %v", tc.from, tc.via, err, ErrInvalidTransition)
			}
		})
	}
}

func TestAllTransitionsContainsAllLegalTriples(t *testing.T) {
	t.Parallel()
	triples := AllTransitions()
	sort.Slice(triples, func(i, j int) bool {
		if triples[i].From != triples[j].From {
			return triples[i].From < triples[j].From
		}
		return triples[i].Via < triples[j].Via
	})
	want := []TransitionTriple{
		{From: StateActive, Via: TransitionExpire, To: StateExpired},
		{From: StateActive, Via: TransitionRevoke, To: StateRevoked},
	}
	if len(triples) != len(want) {
		t.Fatalf("AllTransitions count = %d, want %d", len(triples), len(want))
	}
	for i, tt := range triples {
		if tt != want[i] {
			t.Fatalf("AllTransitions[%d] = %+v, want %+v", i, tt, want[i])
		}
	}
}
