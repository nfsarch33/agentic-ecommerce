// Package bsb validates and formats Australian Bank State Branch numbers.
package bsb

import (
	"errors"
	"strings"
)

// ErrFormat is returned when an input cannot be reduced to exactly six ASCII digits.
var ErrFormat = errors.New("bsb: not six digits")

// Normalise trims surrounding whitespace, strips every space and hyphen from
// the remainder and returns the bare six digits. It returns ErrFormat if the
// cleaned string is not exactly six ASCII digits.
func Normalise(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	var b strings.Builder
	b.Grow(len(trimmed))
	for _, r := range trimmed {
		if r == ' ' || r == '-' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	if len(s) != 6 {
		return "", ErrFormat
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", ErrFormat
		}
	}
	return s, nil
}

// Format applies the Normalise acceptance rule and renders the digits as XXX-XXX.
func Format(raw string) (string, error) {
	n, err := Normalise(raw)
	if err != nil {
		return "", err
	}
	return n[:3] + "-" + n[3:], nil
}

// Valid reports whether raw would be accepted by Normalise. It never panics.
func Valid(raw string) bool {
	_, err := Normalise(raw)
	return err == nil
}
