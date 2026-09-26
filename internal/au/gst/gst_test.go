// Package gst computes Australian GST (10%) in integer cents, never floats.
//
// Contract for the implementer (write gst.go, package gst, standard library only):
//
//	var ErrNegative = errors.New(...)
//	func Add(exCents int64) (incCents int64, err error)
//	func FromInclusive(incCents int64) (exCents, gstCents int64, err error)
//
// Add: GST is 10% of the GST-exclusive amount, rounded to the nearest cent with a half
// cent rounded UP; the result is ex + GST. FromInclusive: GST is 1/11 of the inclusive
// amount, rounded to the nearest cent (half up); ex is inc - GST. A negative input is
// ErrNegative (refunds are the caller's sign convention, not this package's).
package gst

import (
	"errors"
	"testing"
)

func TestAdd(t *testing.T) {
	cases := []struct{ ex, inc int64 }{
		{0, 0},
		{1, 1},       // GST 0.1 c rounds to 0
		{5, 6},       // GST 0.5 c rounds up to 1
		{1000, 1100}, // $10.00 -> $11.00
		{1004, 1104}, // GST 100.4 c -> 100
		{1005, 1106}, // GST 100.5 c -> 101
		{1999, 2199}, // GST 199.9 c -> 200
		{123456, 135802},
	}
	for _, c := range cases {
		got, err := Add(c.ex)
		if err != nil || got != c.inc {
			t.Errorf("Add(%d) = %d, %v; want %d, nil", c.ex, got, err, c.inc)
		}
	}
	if _, err := Add(-1); !errors.Is(err, ErrNegative) {
		t.Errorf("Add(-1) err = %v, want ErrNegative", err)
	}
}

func TestFromInclusive(t *testing.T) {
	cases := []struct{ inc, ex, gst int64 }{
		{0, 0, 0},
		{5, 5, 0}, // 0.45 c -> 0
		{6, 5, 1}, // 0.545 c -> 1
		{11, 10, 1},
		{1000, 909, 91}, // 90.909 c -> 91
		{1100, 1000, 100},
		{2199, 1999, 200}, // 199.909 c -> 200
		{135802, 123456, 12346},
	}
	for _, c := range cases {
		ex, g, err := FromInclusive(c.inc)
		if err != nil || ex != c.ex || g != c.gst {
			t.Errorf("FromInclusive(%d) = (%d, %d, %v); want (%d, %d, nil)", c.inc, ex, g, err, c.ex, c.gst)
		}
		if ex+g != c.inc {
			t.Errorf("FromInclusive(%d): ex + gst = %d, must equal the inclusive amount", c.inc, ex+g)
		}
	}
	if _, _, err := FromInclusive(-11); !errors.Is(err, ErrNegative) {
		t.Errorf("FromInclusive(-11) err = %v, want ErrNegative", err)
	}
}
