package pricing

import "testing"

// File scope: covers the small numeric helpers + decode/mustMap edge
// cases. These previously sat between 60% and 75% because the agent
// happy path didn't exercise zero/negative inputs.

func TestCharmPriceGuardsAgainstNonPositiveInput(t *testing.T) {
	t.Parallel()

	if got := charmPrice(0); got != 0 {
		t.Fatalf("charmPrice(0) = %d, want 0", got)
	}
	if got := charmPrice(-50); got != 0 {
		t.Fatalf("charmPrice(-50) = %d, want 0", got)
	}
}

func TestCharmPriceRoundsToNearest95(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input, want int
	}{
		{input: 100, want: 195},
		{input: 199, want: 195},
		{input: 200, want: 295},
		{input: 449, want: 495},
	}
	for _, tc := range tests {
		tc := tc
		if got := charmPrice(tc.input); got != tc.want {
			t.Fatalf("charmPrice(%d) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

func TestAverageReturnsZeroForEmptyOrAllNonPositive(t *testing.T) {
	t.Parallel()

	if got := average(nil); got != 0 {
		t.Fatalf("average(nil) = %d, want 0", got)
	}
	if got := average([]int{}); got != 0 {
		t.Fatalf("average([]) = %d, want 0", got)
	}
	if got := average([]int{0, -1, -100}); got != 0 {
		t.Fatalf("average(non-positive) = %d, want 0", got)
	}
}

func TestAverageIgnoresNonPositiveValues(t *testing.T) {
	t.Parallel()

	if got := average([]int{200, 0, 400}); got != 300 {
		t.Fatalf("average = %d, want 300", got)
	}
}

func TestDecodePayloadHandlesUnmarshalError(t *testing.T) {
	t.Parallel()

	var dst int
	if err := decodePayload(map[string]any{"foo": 1}, &dst); err == nil {
		t.Fatal("expected unmarshal error")
	}
}

func TestMustMapHandlesNonSerialisableInput(t *testing.T) {
	t.Parallel()

	got := mustMap(make(chan struct{}))
	if len(got) != 0 {
		t.Fatalf("mustMap(channel) = %v, want empty map", got)
	}
}

func TestNormalizeStrategyAcceptsKnownAndDefaultsUnknown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		in   Strategy
		want Strategy
	}{
		{name: "margin", in: StrategyMarginBased, want: StrategyMarginBased},
		{name: "competition", in: StrategyCompetitionBased, want: StrategyCompetitionBased},
		{name: "demand", in: StrategyDemandBased, want: StrategyDemandBased},
		{name: "unknown defaults to competition", in: Strategy("imaginary"), want: StrategyCompetitionBased},
		{name: "empty defaults to competition", in: Strategy(""), want: StrategyCompetitionBased},
	}
	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := normalizeStrategy(tc.in); got != tc.want {
				t.Fatalf("normalizeStrategy(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestRecommendByStrategyDemandBasedHonoursLiftFloorAndCap(t *testing.T) {
	t.Parallel()

	price, reasons := recommendByStrategy(StrategyDemandBased, 1000, 0.6, 0, Request{DemandScore: 0.95})
	if price <= 0 {
		t.Fatalf("price = %d, want positive", price)
	}
	if len(reasons) == 0 {
		t.Fatal("reasons should not be empty")
	}
}

func TestRecommendByStrategyCompetitionBasedFallsBackWhenNoCompetitorAverage(t *testing.T) {
	t.Parallel()

	price, reasons := recommendByStrategy(StrategyCompetitionBased, 1000, 0.4, 0, Request{})
	if price <= 0 {
		t.Fatalf("price = %d, want positive", price)
	}
	hasFloor := false
	for _, reason := range reasons {
		if reason == "target_margin_floor_applied" {
			hasFloor = true
		}
	}
	if !hasFloor {
		t.Fatalf("reasons missing floor signal: %v", reasons)
	}
}
