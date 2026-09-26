// Package auphone normalises Australian mobile and geographic phone numbers to E.164.
package auphone

import (
	"errors"
	"strings"
)

var (
	// ErrFormat is returned for empty input, non-digit characters or the wrong digit count.
	ErrFormat = errors.New("auphone: invalid phone number format")
	// ErrUnsupported is returned for a 9-digit number that is not mobile (4) or geographic (2, 3, 7, 8).
	ErrUnsupported = errors.New("auphone: unsupported phone number")
)

// Normalize converts an Australian mobile or geographic phone number into E.164 form
// ("+61" followed by the 9-digit national number). It accepts spaces, dashes, dots
// and parentheses; a leading "+" is permitted only as the first character. A "61"
// country code (with or without the leading "+") is dropped, and a single trunk "0"
// after the country code or at the start of a 10-digit national number is dropped.
// The remaining digits must be exactly 9, must not start with "0", and must start
// with 2, 3, 4, 7 or 8; otherwise the appropriate sentinel error is returned.
func Normalize(s string) (string, error) {
	if s == "" {
		return "", ErrFormat
	}

	// A leading "+" is allowed only as the first character.
	if s[0] == '+' {
		s = s[1:]
	}

	// Remove spaces, dashes, dots and parentheses.
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch r {
		case ' ', '-', '.', '(', ')':
			continue
		default:
			b.WriteRune(r)
		}
	}
	s = b.String()

	if s == "" {
		return "", ErrFormat
	}

	// Every remaining character must be an ASCII digit. A "+" anywhere except the
	// very first position fails this check and yields ErrFormat.
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return "", ErrFormat
		}
	}

	// Drop the "61" country code if present.
	if strings.HasPrefix(s, "61") {
		s = s[2:]
	}

	// Drop a trunk "0" from a 10-digit national number starting with "0".
	if len(s) == 10 && s[0] == '0' {
		s = s[1:]
	}

	if len(s) != 9 {
		return "", ErrFormat
	}
	if s[0] == '0' {
		return "", ErrFormat
	}

	switch s[0] {
	case '2', '3', '4', '7', '8':
		return "+61" + s, nil
	default:
		return "", ErrUnsupported
	}
}

// IsMobile reports whether s normalises to an Australian mobile number (a number
// whose 9-digit national component starts with 4).
func IsMobile(s string) bool {
	n, err := Normalize(s)
	if err != nil {
		return false
	}
	// n is "+61" followed by 9 digits; index 3 is the first national digit.
	return len(n) >= 4 && n[3] == '4'
}
