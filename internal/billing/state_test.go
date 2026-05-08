package billing

import (
	"errors"
	"testing"
)

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
		{"resume from active", StateActive, TransitionResume},
		{"activate from canceled", StateCanceled, TransitionActivate},
		{"any from canceled", StateCanceled, TransitionPause},
		{"pause from past_due", StatePastDue, TransitionPause},
		{"recover from active", StateActive, TransitionRecover},
		{"resume from past_due", StatePastDue, TransitionResume},
		{"mark_past_due from paused", StatePaused, TransitionMarkPastDue},
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
	for _, ok := range []string{"trialing", "active", "past_due", "paused", "canceled"} {
		if _, err := ParseState(ok); err != nil {
			t.Fatalf("ParseState(%q) failed: %v", ok, err)
		}
	}
	if _, err := ParseState("garbage"); !errors.Is(err, ErrInvalidState) {
		t.Fatalf("expected ErrInvalidState, got %v", err)
	}
	for _, ok := range []string{"activate", "mark_past_due", "recover", "pause", "resume", "cancel"} {
		if _, err := ParseTransition(ok); err != nil {
			t.Fatalf("ParseTransition(%q) failed: %v", ok, err)
		}
	}
	if _, err := ParseTransition("garbage"); !errors.Is(err, ErrInvalidTransitionName) {
		t.Fatalf("expected ErrInvalidTransitionName, got %v", err)
	}
}

func TestStateIsTerminal(t *testing.T) {
	t.Parallel()
	if !StateCanceled.IsTerminal() {
		t.Fatalf("canceled must be terminal")
	}
	if StateActive.IsTerminal() {
		t.Fatalf("active must not be terminal")
	}
}

func TestParseInvoiceStatus(t *testing.T) {
	t.Parallel()
	for _, ok := range []string{"open", "paid", "void", "uncollectible"} {
		if _, err := ParseInvoiceStatus(ok); err != nil {
			t.Fatalf("ParseInvoiceStatus(%q) failed: %v", ok, err)
		}
	}
	if _, err := ParseInvoiceStatus("garbage"); !errors.Is(err, ErrInvalidInvoiceStatus) {
		t.Fatalf("expected ErrInvalidInvoiceStatus, got %v", err)
	}
}
