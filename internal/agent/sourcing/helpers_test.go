package sourcing

import "testing"

// File scope: covers the small numeric helpers (grossMargin, clamp01,
// round2, round4) and decodePayload edge cases. These previously sat
// between 60% and 75% because Run() exercised only the happy path.

func TestGrossMarginGuardsAgainstZeroSellPrice(t *testing.T) {
	t.Parallel()

	if got := grossMargin(100, 0); got != 0 {
		t.Fatalf("grossMargin(100, 0) = %v, want 0", got)
	}
	if got := grossMargin(100, -50); got != 0 {
		t.Fatalf("grossMargin(100, -50) = %v, want 0", got)
	}
}

func TestGrossMarginComputesPositiveMargin(t *testing.T) {
	t.Parallel()

	if got := grossMargin(40, 100); got != 0.6 {
		t.Fatalf("grossMargin(40, 100) = %v, want 0.6", got)
	}
}

func TestClamp01ClampsAboveAndBelow(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input, want float64
	}{
		{input: -0.5, want: 0},
		{input: 0, want: 0},
		{input: 0.5, want: 0.5},
		{input: 1.0, want: 1.0},
		{input: 1.5, want: 1.0},
	}
	for _, tc := range tests {
		tc := tc
		if got := clamp01(tc.input); got != tc.want {
			t.Fatalf("clamp01(%v) = %v, want %v", tc.input, got, tc.want)
		}
	}
}

func TestRound2AndRound4Behavior(t *testing.T) {
	t.Parallel()

	if got := round2(1.23456); got != 1.23 {
		t.Fatalf("round2(1.23456) = %v, want 1.23", got)
	}
	if got := round4(1.23456789); got != 1.2346 {
		t.Fatalf("round4(1.23456789) = %v, want 1.2346", got)
	}
}

func TestDecodePayloadHandlesUnmarshalError(t *testing.T) {
	t.Parallel()

	// An int destination cannot accept the marshalled map, exercising
	// the unmarshal error branch.
	var dst int
	if err := decodePayload(map[string]any{"sku": "RB"}, &dst); err == nil {
		t.Fatal("expected decodePayload to surface unmarshal error")
	}
}

func TestMustMapReturnsEmptyMapForNonSerialisableInput(t *testing.T) {
	t.Parallel()

	got := mustMap(make(chan int))
	if len(got) != 0 {
		t.Fatalf("mustMap(channel) = %v, want empty map", got)
	}
}

func TestMustMapRoundTripsStruct(t *testing.T) {
	t.Parallel()

	type sample struct {
		SKU string `json:"sku"`
	}
	got := mustMap(sample{SKU: "RB"})
	if got["sku"] != "RB" {
		t.Fatalf("mustMap = %v, want sku=RB", got)
	}
}
