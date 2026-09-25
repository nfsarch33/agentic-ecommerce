// Package audwords renders an amount of money in Australian dollars as
// English words, the wording an invoice or cheque line carries.
package audwords

import (
	"errors"
	"strings"
)

var (
	ErrNegative = errors.New("audwords: negative amount")
	ErrTooLarge = errors.New("audwords: amount over 999999999 dollars")
)

var (
	ones  = []string{"", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine"}
	teens = []string{"ten", "eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}
	tens  = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}
)

// renderBelow100 renders 1..99. 0 returns the empty string so callers can
// decide what "zero" looks like in context.
func renderBelow100(n int) string {
	if n < 10 {
		return ones[n]
	}
	if n < 20 {
		return teens[n-10]
	}
	t, u := n/10, n%10
	if u == 0 {
		return tens[t]
	}
	return tens[t] + "-" + ones[u]
}

// renderBelow1000 renders 1..999, with a hyphenated tens-units tail and an
// optional "<hundreds> hundred" prefix; 0 returns "".
func renderBelow1000(n int) string {
	if n == 0 {
		return ""
	}
	if n < 100 {
		return renderBelow100(n)
	}
	h, rest := n/100, n%100
	if rest == 0 {
		return ones[h] + " hundred"
	}
	return ones[h] + " hundred " + renderBelow100(rest)
}

// Words renders an amount in cents as Australian-dollar invoice wording:
// "<dollars> dollars and <cents> cents", with no "and" inside numbers, a
// hyphenated tens-units tail, and group words ("million", "thousand")
// emitted only when the group is non-zero.
func Words(cents int64) (string, error) {
	if cents < 0 {
		return "", ErrNegative
	}
	if cents > 99999999999 {
		return "", ErrTooLarge
	}

	dollars := cents / 100
	c := int(cents % 100)

	millions := int(dollars / 1000000)
	thousands := int((dollars / 1000) % 1000)
	rest := int(dollars % 1000)

	var dparts []string
	if millions > 0 {
		dparts = append(dparts, renderBelow1000(millions)+" million")
	}
	if thousands > 0 {
		dparts = append(dparts, renderBelow1000(thousands)+" thousand")
	}
	if rest > 0 {
		dparts = append(dparts, renderBelow1000(rest))
	}
	if len(dparts) == 0 {
		dparts = append(dparts, "zero")
	}

	var cword string
	if c == 0 {
		cword = "zero"
	} else {
		cword = renderBelow100(c)
	}

	return strings.Join(dparts, " ") + " dollars and " + cword + " cents", nil
}
