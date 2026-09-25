// Package abn validates and formats Australian Business Numbers.
//
// Contract for the implementer (write abn.go, package abn, standard library only):
//
//	var ErrFormat = errors.New(...)        // not 11 digits after removing spaces
//	func Normalize(s string) (string, error) // spaces removed; exactly 11 ASCII digits, else ErrFormat
//	func Valid(s string) bool                // Normalize succeeds AND the ATO checksum holds
//	func Format(s string) (string, error)    // "51 824 753 556" (2-3-3-3); ErrFormat if Normalize fails
//
// ATO checksum: subtract 1 from the FIRST digit, multiply the 11 digits by the weights
// 10, 1, 3, 5, 7, 9, 11, 13, 15, 17, 19, add the products; the ABN is valid when the
// sum is divisible by 89. Only spaces are removed; any other character is ErrFormat.
package abn

import (
	"errors"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		in, want string
		err      bool
	}{
		{"51 824 753 556", "51824753556", false},
		{"51824753556", "51824753556", false},
		{"  53 004 085 616 ", "53004085616", false},
		{"5182475355", "", true},     // 10 digits
		{"518247535561", "", true},   // 12 digits
		{"51-824-753-556", "", true}, // dashes are not removed
		{"51 824 753 55a", "", true},
		{"", "", true},
	}
	for _, c := range cases {
		got, err := Normalize(c.in)
		if c.err {
			if !errors.Is(err, ErrFormat) {
				t.Errorf("Normalize(%q) err = %v, want ErrFormat", c.in, err)
			}
			continue
		}
		if err != nil || got != c.want {
			t.Errorf("Normalize(%q) = %q, %v; want %q, nil", c.in, got, err, c.want)
		}
	}
}

func TestValid(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"51 824 753 556", true},
		{"51824753556", true},
		{"53 004 085 616", true},
		{"51 824 753 557", false}, // last digit changed: checksum fails
		{"15 824 753 556", false}, // first two digits swapped
		{"5182475355", false},     // too short
		{"abc", false},
		{"", false},
	}
	for _, c := range cases {
		if got := Valid(c.in); got != c.want {
			t.Errorf("Valid(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestFormat(t *testing.T) {
	got, err := Format("51824753556")
	if err != nil || got != "51 824 753 556" {
		t.Fatalf("Format = %q, %v; want \"51 824 753 556\", nil", got, err)
	}
	got, err = Format(" 53 004 085 616")
	if err != nil || got != "53 004 085 616" {
		t.Fatalf("Format = %q, %v; want \"53 004 085 616\", nil", got, err)
	}
	if _, err := Format("123"); !errors.Is(err, ErrFormat) {
		t.Fatalf("Format(\"123\") err = %v, want ErrFormat", err)
	}
}
