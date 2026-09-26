// Package bsb validates and formats Australian Bank State Branch numbers -
// the 6-digit routing identifiers on Australian bank accounts, conventionally
// written as two triplets separated by a hyphen (062-000).
//
// Contract for the implementer (write bsb.go, package bsb, standard library
// only; declare the sentinel with errors.New):
//
//	var ErrFormat = errors.New("bsb: not six digits")
//
//	func Normalise(raw string) (string, error)
//	  - trim surrounding whitespace
//	  - strip every space and hyphen anywhere in the string
//	  - the remainder must be exactly six ASCII digits, else ErrFormat
//	  - return the bare six digits
//
//	func Format(raw string) (string, error)
//	  - same acceptance rule as Normalise
//	  - return the digits as "XXX-XXX"
//
//	func Valid(raw string) bool
//	  - true exactly when Normalise would succeed; never panics
package bsb

import (
	"errors"
	"testing"
)

func TestNormalise(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"already bare", "062000", "062000"},
		{"hyphen form", "062-000", "062000"},
		{"surrounding space", " 062-000 ", "062000"},
		{"inner spaces", "062 000", "062000"},
		{"messy mix", " 0 6-2--0 0 0 ", "062000"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Normalise(tc.in)
			if err != nil {
				t.Fatalf("Normalise(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("Normalise(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormaliseRejects(t *testing.T) {
	for _, in := range []string{"", "  ", "06200", "0620000", "06200a", "06.2000"} {
		if _, err := Normalise(in); !errors.Is(err, ErrFormat) {
			t.Errorf("Normalise(%q) err = %v, want ErrFormat", in, err)
		}
	}
}

func TestFormat(t *testing.T) {
	got, err := Format("062 000")
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if got != "062-000" {
		t.Errorf("Format = %q, want 062-000", got)
	}
	if got, _ := Format("123456"); got != "123-456" {
		t.Errorf("Format(123456) = %q, want 123-456", got)
	}
}

func TestValid(t *testing.T) {
	cases := map[string]bool{
		"062-000":   true,
		"062000":    true,
		" 062 000 ": true,
		"-062000-":  true,
		"06200":     false,
		"0620001":   false,
		"abcdef":    false,
		"":          false,
		"-":         false,
	}
	for in, want := range cases {
		if got := Valid(in); got != want {
			t.Errorf("Valid(%q) = %v, want %v", in, got, want)
		}
	}
}
