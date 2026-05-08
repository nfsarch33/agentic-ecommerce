package membership

import (
	"errors"
	"testing"
	"time"
)

func TestParseBillingCycle(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		input   string
		want    BillingCycle
		wantErr error
	}{
		{name: "monthly", input: "monthly", want: BillingCycleMonthly},
		{name: "monthly with whitespace", input: "  Monthly  ", want: BillingCycleMonthly},
		{name: "quarterly", input: "quarterly", want: BillingCycleQuarterly},
		{name: "annual", input: "ANNUAL", want: BillingCycleAnnual},
		{name: "weekly invalid", input: "weekly", wantErr: ErrInvalidBillingCycle},
		{name: "empty invalid", input: "", wantErr: ErrInvalidBillingCycle},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := ParseBillingCycle(tc.input)
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

func TestBillingCycleDuration(t *testing.T) {
	t.Parallel()

	cases := map[BillingCycle]time.Duration{
		BillingCycleMonthly:   30 * 24 * time.Hour,
		BillingCycleQuarterly: 90 * 24 * time.Hour,
		BillingCycleAnnual:    365 * 24 * time.Hour,
		BillingCycle("bogus"): 0,
	}
	for cycle, want := range cases {
		cycle, want := cycle, want
		t.Run(string(cycle), func(t *testing.T) {
			t.Parallel()
			if got := cycle.Duration(); got != want {
				t.Fatalf("%s.Duration() = %s, want %s", cycle, got, want)
			}
		})
	}
}

func TestBillingCycleString(t *testing.T) {
	t.Parallel()
	if BillingCycleMonthly.String() != "monthly" {
		t.Fatalf("monthly string mismatch: %s", BillingCycleMonthly.String())
	}
}
