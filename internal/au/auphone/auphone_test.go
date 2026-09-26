// Package auphone normalises Australian mobile and geographic phone numbers to E.164.
//
// Contract for the implementer (write auphone.go, package auphone, standard library only):
//
//	var ErrFormat = errors.New(...)      // empty, letters, or the wrong number of digits
//	var ErrUnsupported = errors.New(...) // 9 digits, but not a mobile (4) or geographic (2, 3, 7, 8) number
//	func Normalize(s string) (string, error) // "+61" followed by the 9-digit national number
//	func IsMobile(s string) bool             // Normalize succeeds and the national number starts with 4
//
// Rules: remove spaces, dashes, dots and parentheses; a leading "+" is allowed only as
// the first character. Then: "+61..." or "61..." (11 digits) drop the country code;
// after the country code a single trunk "0" is dropped ("+61 (0)412 ..."); a 10-digit
// number starting with "0" drops that "0". What remains must be exactly 9 digits and must
// not start with "0" (else ErrFormat); its first digit must be 2, 3, 4, 7 or 8 (else
// ErrUnsupported: 13/1300/1800 numbers have no E.164 form here).
package auphone

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	ok := []struct{ in, want string }{
		{"0412 345 678", "+61412345678"},
		{"0412-345-678", "+61412345678"},
		{"+61 412 345 678", "+61412345678"},
		{"61412345678", "+61412345678"},
		{"+61 (0)412 345 678", "+61412345678"},
		{"(02) 9876 5432", "+61298765432"},
		{"02.9876.5432", "+61298765432"},
		{"+61 3 9123 4567", "+61391234567"},
		{"07 3000 1234", "+61730001234"},
		{"08 9123 4567", "+61891234567"},
	}
	for _, c := range ok {
		got, err := Normalize(c.in)
		if err != nil || got != c.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}
	bad := []struct {
		in   string
		want error
	}{
		{"", ErrFormat},
		{"0412 345 67", ErrFormat},   // 9 digits with a trunk 0 -> 8 left
		{"0412 345 6789", ErrFormat}, // too long
		{"04I2 345 678", ErrFormat},  // a letter I, not a digit 1
		{"61+412345678", ErrFormat},  // + not first
		{"1300 123 456", ErrFormat},  // 10 digits not starting with 0
		{"0512 345 678", ErrUnsupported},
		{"+61 1300 123 45", ErrUnsupported},
	}
	for _, c := range bad {
		if _, err := Normalize(c.in); !errors.Is(err, c.want) {
			t.Errorf("Normalize(%q) err = %v, want %v", c.in, err, c.want)
		}
	}
}

func TestIsMobile(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"0412 345 678", true},
		{"+61412345678", true},
		{"(02) 9876 5432", false},
		{"not a number", false},
	}
	for _, c := range cases {
		if got := IsMobile(c.in); got != c.want {
			t.Errorf("IsMobile(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
