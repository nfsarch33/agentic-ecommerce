// Package abn validates and formats Australian Business Numbers.
package abn

import "errors"

// ErrFormat is returned when input cannot be normalized to exactly 11 ASCII digits.
var ErrFormat = errors.New("abn: invalid format")

// abnWeights are the ATO mod-89 weighting factors applied to the 11 ABN digits.
var abnWeights = [11]int{10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19}

// Normalize removes spaces from s and verifies the result is exactly 11 ASCII digits.
func Normalize(s string) (string, error) {
	if len(s) < 11 {
		return "", ErrFormat
	}
	digits := make([]byte, 0, 11)
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' {
			continue
		}
		if c < '0' || c > '9' {
			return "", ErrFormat
		}
		digits = append(digits, c)
	}
	if len(digits) != 11 {
		return "", ErrFormat
	}
	return string(digits), nil
}

// Valid reports whether s is a syntactically valid ABN that also satisfies the
// ATO mod-89 checksum.
func Valid(s string) bool {
	digits, err := Normalize(s)
	if err != nil {
		return false
	}
	sum := (int(digits[0]-'0') - 1) * abnWeights[0]
	for i := 1; i < 11; i++ {
		sum += int(digits[i]-'0') * abnWeights[i]
	}
	return sum%89 == 0
}

// Format returns s in the canonical "NN NNN NNN NNN" presentation, or ErrFormat
// if s cannot be normalized.
func Format(s string) (string, error) {
	digits, err := Normalize(s)
	if err != nil {
		return "", err
	}
	return digits[0:2] + " " + digits[2:5] + " " + digits[5:8] + " " + digits[8:11], nil
}
