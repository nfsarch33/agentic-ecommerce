// Package costcalc turns model token counts into Australian-dollar cents from
// a per-model price table and exposes the two alert predicates the cost
// ledger uses. All arithmetic is integer: no float anywhere.
package costcalc

import (
	"errors"
	"math"
	"sort"
	"time"
)

// Price is a model's per-million-token price in Australian-dollar cents.
type Price struct {
	InputCentsPerMTok  int64
	OutputCentsPerMTok int64
}

// Table maps a model name to its Price. A zero Price is a valid entry that
// prices a local tier at zero.
type Table map[string]Price

// Sentinel errors returned by Table.Cost. They are distinct so callers can
// branch on them with errors.Is.
var (
	ErrUnknownModel   = errors.New("costcalc: unknown model")
	ErrNegativeTokens = errors.New("costcalc: negative token count")
	ErrNegativePrice  = errors.New("costcalc: negative price")
	ErrOverflow       = errors.New("costcalc: arithmetic overflow")
)

// centsPerMTok is the divisor applied to the raw token*price numerator.
const centsPerMTok int64 = 1_000_000

// Cost returns the price in Australian-dollar cents for tokensIn input and
// tokensOut output tokens against model. Any non-zero cost is rounded UP to
// the next whole cent. Errors are checked in this order: unknown model,
// negative tokens, negative price, overflow. On error cents is 0.
func (t Table) Cost(model string, tokensIn, tokensOut int64) (int64, error) {
	p, ok := t[model]
	if !ok {
		return 0, ErrUnknownModel
	}
	if tokensIn < 0 || tokensOut < 0 {
		return 0, ErrNegativeTokens
	}
	if p.InputCentsPerMTok < 0 || p.OutputCentsPerMTok < 0 {
		return 0, ErrNegativePrice
	}
	prodIn, err := mulCheck(tokensIn, p.InputCentsPerMTok)
	if err != nil {
		return 0, err
	}
	prodOut, err := mulCheck(tokensOut, p.OutputCentsPerMTok)
	if err != nil {
		return 0, err
	}
	sum, err := addCheck(prodIn, prodOut)
	if err != nil {
		return 0, err
	}
	if sum == 0 {
		return 0, nil
	}
	q := sum / centsPerMTok
	if sum%centsPerMTok != 0 {
		q++
	}
	return q, nil
}

// mulCheck returns a*b, or ErrOverflow if the exact product would not fit in
// int64. Inputs may be negative; the absolute values are bounded by
// math.MaxInt64.
func mulCheck(a, b int64) (int64, error) {
	if a == 0 || b == 0 {
		return 0, nil
	}
	absA := a
	if absA < 0 {
		absA = -absA
	}
	absB := b
	if absB < 0 {
		absB = -absB
	}
	if absA > math.MaxInt64/absB {
		return 0, ErrOverflow
	}
	return a * b, nil
}

// addCheck returns a+b, or ErrOverflow if the exact sum would not fit in
// int64.
func addCheck(a, b int64) (int64, error) {
	if b > 0 && a > math.MaxInt64-b {
		return 0, ErrOverflow
	}
	if b < 0 && a < math.MinInt64-b {
		return 0, ErrOverflow
	}
	return a + b, nil
}

// OverDailyThreshold reports whether spentCents has reached or passed
// thresholdCents. A non-positive threshold turns the alert off.
func OverDailyThreshold(spentCents, thresholdCents int64) bool {
	return thresholdCents > 0 && spentCents >= thresholdCents
}

// RetryStorm reports whether some inclusive time span of length window
// contains at least limit attempts. attempts may be unsorted and is not
// modified. A non-positive window or limit turns the check off.
func RetryStorm(attempts []time.Time, window time.Duration, limit int) bool {
	if window <= 0 || limit <= 0 {
		return false
	}
	n := len(attempts)
	if n < limit {
		return false
	}
	sorted := make([]time.Time, n)
	copy(sorted, attempts)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Before(sorted[j])
	})
	for i := 0; i+limit <= n; i++ {
		if sorted[i+limit-1].Sub(sorted[i]) <= window {
			return true
		}
	}
	return false
}
