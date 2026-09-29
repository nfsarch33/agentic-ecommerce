// Contract for package costcalc (author-written; the implementer may not edit this file).
//
// costcalc turns model token counts into Australian-dollar cents from a price table, and holds
// the two alert predicates the cost ledger uses. Integer arithmetic only: no float anywhere.
//
//	type Price struct {
//	    InputCentsPerMTok  int64 // AUD cents per 1,000,000 input tokens
//	    OutputCentsPerMTok int64 // AUD cents per 1,000,000 output tokens
//	}
//	type Table map[string]Price // model name -> price; a zero Price is a real entry (local tier)
//
//	var ErrUnknownModel   = errors.New(...) // model not in the table (never priced as 0)
//	var ErrNegativeTokens = errors.New(...) // tokensIn or tokensOut < 0
//	var ErrNegativePrice  = errors.New(...) // the model's Price has a negative field
//	var ErrOverflow       = errors.New(...) // an intermediate product would overflow int64
//
//	func (t Table) Cost(model string, tokensIn, tokensOut int64) (int64, error)
//	    cents = ceil((tokensIn*InputCentsPerMTok + tokensOut*OutputCentsPerMTok) / 1_000_000)
//	    rounded UP to the whole cent: any non-zero cost is at least 1 cent. Errors are checked in
//	    this order: unknown model, negative tokens, negative price, overflow; on error cents is 0.
//	    Overflow means tokensIn*InputCentsPerMTok, tokensOut*OutputCentsPerMTok or their sum
//	    exceeds math.MaxInt64.
//
//	func OverDailyThreshold(spentCents, thresholdCents int64) bool
//	    true when thresholdCents > 0 and spentCents >= thresholdCents; a threshold <= 0 is off.
//
//	func RetryStorm(attempts []time.Time, window time.Duration, limit int) bool
//	    true when some span of length window (inclusive at both ends) contains at least limit
//	    attempts. attempts may be unsorted and must not be modified. window <= 0 or limit <= 0
//	    is off (false).
package costcalc

import (
	"errors"
	"math"
	"testing"
	"time"
)

var table = Table{
	"MiniMax-M3":        {InputCentsPerMTok: 42, OutputCentsPerMTok: 168},
	"qwen3.8-27b-local": {},
	"broken":            {InputCentsPerMTok: -1, OutputCentsPerMTok: 5},
}

func TestCostRoundsUpToWholeCents(t *testing.T) {
	cases := []struct {
		name     string
		in, out  int64
		wantCent int64
	}{
		{"exact million input", 1_000_000, 0, 42},
		{"exact million output", 0, 1_000_000, 168},
		{"both", 2_000_000, 500_000, 84 + 84},
		{"one token is still one cent", 1, 0, 1},
		{"just under a cent boundary rounds up", 1_000_000, 1, 43},
		{"zero tokens cost nothing", 0, 0, 0},
		{"typical call", 34_000, 1_200, 2}, // 34000*42 + 1200*168 = 1,629,600 -> 1.6296 -> 2
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := table.Cost("MiniMax-M3", c.in, c.out)
			if err != nil {
				t.Fatalf("Cost: %v", err)
			}
			if got != c.wantCent {
				t.Fatalf("Cost(%d, %d) = %d, want %d", c.in, c.out, got, c.wantCent)
			}
		})
	}
}

func TestLocalTierIsAnExplicitZero(t *testing.T) {
	got, err := table.Cost("qwen3.8-27b-local", 5_000_000, 5_000_000)
	if err != nil || got != 0 {
		t.Fatalf("local tier = %d, %v; want 0, nil", got, err)
	}
}

func TestUnknownModelIsAnErrorNeverZero(t *testing.T) {
	got, err := table.Cost("gpt-unknown", 10, 10)
	if !errors.Is(err, ErrUnknownModel) || got != 0 {
		t.Fatalf("unknown model = %d, %v; want 0, ErrUnknownModel", got, err)
	}
	var empty Table
	if _, err := empty.Cost("MiniMax-M3", 1, 1); !errors.Is(err, ErrUnknownModel) {
		t.Fatalf("nil table: %v, want ErrUnknownModel", err)
	}
}

func TestCostErrorsAndOrder(t *testing.T) {
	cases := []struct {
		name    string
		model   string
		in, out int64
		err     error
	}{
		{"negative input", "MiniMax-M3", -1, 0, ErrNegativeTokens},
		{"negative output", "MiniMax-M3", 0, -1, ErrNegativeTokens},
		{"negative price", "broken", 1, 1, ErrNegativePrice},
		{"unknown model wins over negative tokens", "nope", -1, -1, ErrUnknownModel},
		{"negative tokens win over negative price", "broken", -5, 0, ErrNegativeTokens},
		{"input product overflows", "MiniMax-M3", math.MaxInt64 / 10, 0, ErrOverflow},
		{"output product overflows", "MiniMax-M3", 0, math.MaxInt64 / 100, ErrOverflow},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := table.Cost(c.model, c.in, c.out)
			if !errors.Is(err, c.err) {
				t.Fatalf("err = %v, want %v", err, c.err)
			}
			if got != 0 {
				t.Fatalf("cents on error = %d, want 0", got)
			}
		})
	}
}

func TestSumOverflowIsCaught(t *testing.T) {
	big := Table{"m": {InputCentsPerMTok: 1, OutputCentsPerMTok: 1}}
	// Each product fits in int64; their sum does not.
	half := int64(math.MaxInt64/2) + 1
	if _, err := big.Cost("m", half, half); !errors.Is(err, ErrOverflow) {
		t.Fatalf("sum overflow: err = %v, want ErrOverflow", err)
	}
	// The largest sum that fits is fine.
	got, err := big.Cost("m", math.MaxInt64-1_000_000, 0)
	if err != nil || got <= 0 {
		t.Fatalf("near-max = %d, %v; want a positive cost", got, err)
	}
}

func TestSentinelErrorsAreDistinct(t *testing.T) {
	all := []error{ErrUnknownModel, ErrNegativeTokens, ErrNegativePrice, ErrOverflow}
	for i := range all {
		if all[i] == nil {
			t.Fatalf("sentinel %d is nil", i)
		}
		for j := range all {
			if i != j && errors.Is(all[i], all[j]) {
				t.Fatalf("sentinels %d and %d are the same error", i, j)
			}
		}
	}
}

func TestOverDailyThreshold(t *testing.T) {
	cases := []struct {
		spent, threshold int64
		want             bool
	}{
		{999, 1000, false},
		{1000, 1000, true},
		{1001, 1000, true},
		{5000, 0, false},
		{5000, -1, false},
		{0, 1, false},
	}
	for _, c := range cases {
		if got := OverDailyThreshold(c.spent, c.threshold); got != c.want {
			t.Fatalf("OverDailyThreshold(%d, %d) = %v, want %v", c.spent, c.threshold, got, c.want)
		}
	}
}

func TestRetryStorm(t *testing.T) {
	t0 := time.Date(2026, 9, 28, 9, 0, 0, 0, time.UTC)
	at := func(sec ...int) []time.Time {
		out := make([]time.Time, 0, len(sec))
		for _, s := range sec {
			out = append(out, t0.Add(time.Duration(s)*time.Second))
		}
		return out
	}
	cases := []struct {
		name   string
		times  []time.Time
		window time.Duration
		limit  int
		want   bool
	}{
		{"three within a minute", at(0, 20, 40), time.Minute, 3, true},
		{"window end is inclusive", at(0, 30, 60), time.Minute, 3, true},
		{"just outside the window", at(0, 30, 61), time.Minute, 3, false},
		{"unsorted input", at(40, 0, 20), time.Minute, 3, true},
		{"sliding: the burst is late", at(0, 300, 310, 320), time.Minute, 3, true},
		{"too few attempts", at(0, 10), time.Minute, 3, false},
		{"limit off", at(0, 1, 2), time.Minute, 0, false},
		{"window off", at(0, 0, 0), 0, 1, false},
		{"empty", nil, time.Minute, 1, false},
		{"limit one with one attempt", at(0), time.Minute, 1, true},
		{"21-call loop in a day", at(0, 60, 120, 180, 240, 300, 360, 420, 480, 540, 600, 660, 720, 780, 840, 900, 960, 1020, 1080, 1140, 1200), 24 * time.Hour, 21, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			before := append([]time.Time(nil), c.times...)
			if got := RetryStorm(c.times, c.window, c.limit); got != c.want {
				t.Fatalf("RetryStorm = %v, want %v", got, c.want)
			}
			for i := range before {
				if !before[i].Equal(c.times[i]) {
					t.Fatal("RetryStorm must not modify its input")
				}
			}
		})
	}
}

func FuzzCostNeverNegative(f *testing.F) {
	f.Add(int64(0), int64(0), int64(42), int64(168))
	f.Add(int64(1), int64(1), int64(1), int64(1))
	f.Add(int64(math.MaxInt64), int64(0), int64(1), int64(0))
	f.Fuzz(func(t *testing.T, in, out, pin, pout int64) {
		tb := Table{"m": {InputCentsPerMTok: pin, OutputCentsPerMTok: pout}}
		got, err := tb.Cost("m", in, out)
		if err != nil {
			if got != 0 {
				t.Fatalf("cents on error = %d", got)
			}
			return
		}
		if got < 0 {
			t.Fatalf("Cost(%d,%d @ %d,%d) = %d < 0", in, out, pin, pout, got)
		}
	})
}
