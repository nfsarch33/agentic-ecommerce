package marketplace

import (
	"errors"
	"testing"
)

// TestAllTransitions covers every legal transition triple in the
// table, the inverse for terminal-from rejection, and parsing.
func TestAllTransitions(t *testing.T) {
	t.Parallel()
	triples := AllTransitions()
	if len(triples) == 0 {
		t.Fatal("transition table is empty")
	}
	for _, tt := range triples {
		got, err := nextState(tt.From, tt.Via)
		if err != nil {
			t.Fatalf("legal triple %v -> err %v", tt, err)
		}
		if got != tt.To {
			t.Fatalf("legal triple %v -> got %s want %s", tt, got, tt.To)
		}
	}
}

func TestInvalidTransitions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		from State
		via  Transition
	}{
		{"deactivate from installed", StateInstalled, TransitionDeactivate},
		{"activate from uninstalled", StateUninstalled, TransitionActivate},
		{"deactivate from uninstalled", StateUninstalled, TransitionDeactivate},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := nextState(tc.from, tc.via)
			if !errors.Is(err, ErrInvalidTransition) {
				t.Fatalf("expected ErrInvalidTransition, got %v", err)
			}
		})
	}
}

func TestParseStateAndTransition(t *testing.T) {
	t.Parallel()
	if _, err := ParseState("installed"); err != nil {
		t.Fatalf("ParseState ok input failed: %v", err)
	}
	if _, err := ParseState("garbage"); !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected ErrInvalidTransition for garbage state, got %v", err)
	}
	if _, err := ParseTransition("activate"); err != nil {
		t.Fatalf("ParseTransition ok input failed: %v", err)
	}
	if _, err := ParseTransition("garbage"); !errors.Is(err, ErrInvalidTransitionName) {
		t.Fatalf("expected ErrInvalidTransitionName, got %v", err)
	}
}

func TestStateIsTerminal(t *testing.T) {
	t.Parallel()
	if StateUninstalled.IsTerminal() == false {
		t.Fatalf("uninstalled must be terminal")
	}
	if StateActive.IsTerminal() == true {
		t.Fatalf("active must not be terminal")
	}
}
