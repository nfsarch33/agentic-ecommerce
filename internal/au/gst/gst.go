// Package gst computes Australian GST (10%) in integer cents, never floats.
package gst

import (
	"errors"
	"math"
)

// ErrNegative is returned when the input is negative. Refunds are the caller's
// sign convention; this package deals only with non-negative amounts.
var ErrNegative = errors.New("gst: amount is negative")

// ErrTooLarge is returned when the GST-inclusive total would overflow
// int64 (Add used to wrap negative with a nil error near the
// ceiling). The largest safe exclusive amount is (MaxInt64-1)*10/11.
var ErrTooLarge = errors.New("gst: amount too large")

// Add returns the GST-inclusive amount for a GST-exclusive amount in cents.
// GST is 10% of the exclusive amount, rounded to the nearest cent with a
// half cent rounded UP; the result is ex + GST.
func Add(exCents int64) (int64, error) {
	if exCents < 0 {
		return 0, ErrNegative
	}
	if exCents > (math.MaxInt64-1)*10/11 {
		return 0, ErrTooLarge
	}
	q := exCents / 10
	r := exCents % 10
	gst := q
	if 2*r >= 10 {
		gst++
	}
	return exCents + gst, nil
}

// FromInclusive returns the GST-exclusive amount and the GST component of a
// GST-inclusive amount in cents. GST is 1/11 of the inclusive amount, rounded
// to the nearest cent (half up); ex is inc - GST.
func FromInclusive(incCents int64) (int64, int64, error) {
	if incCents < 0 {
		return 0, 0, ErrNegative
	}
	q := incCents / 11
	r := incCents % 11
	gst := q
	if 2*r >= 11 {
		gst++
	}
	ex := incCents - gst
	return ex, gst, nil
}
