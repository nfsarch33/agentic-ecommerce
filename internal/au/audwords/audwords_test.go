// Package audwords renders an amount of money in Australian dollars as
// English words, the wording an invoice or cheque line carries.
//
// Contract for the implementer (write audwords.go, package audwords,
// standard library only; declare the sentinels with errors.New):
//
//		var ErrNegative = errors.New("audwords: negative amount")
//		var ErrTooLarge = errors.New("audwords: amount over 999999999 dollars")
//
//		func Words(cents int64) (string, error)
//	  cents is the amount in cents: 12345 means 123 dollars and 45 cents.
//	  - ErrNegative when cents < 0
//	  - ErrTooLarge when cents > 99999999999 (i.e. dollars > 999,999,999)
//	  - the dollars part in words with NO "and": 204 -> "two hundred four",
//	    1234 -> "one thousand two hundred thirty-four"
//	  - tens+units under 100 hyphenated: 45 -> "forty-five", 21 -> "twenty-one"
//	  - 100 -> "one hundred", 1000 -> "one thousand" (no trailing unit word
//	    when the lower group is zero)
//	  - cents part rendered the same way, always plain ("zero" when cents==0)
//	  - joined as "<dollars> dollars and <cents> cents"
//	  - exactly zero: "zero dollars and zero cents"
//	  - e.g. 12345 -> "one hundred twenty-three dollars and forty-five cents"
package audwords

import (
	"errors"
	"testing"
)

func TestWords(t *testing.T) {
	tests := []struct {
		name  string
		cents int64
		want  string
	}{
		{"zero", 0, "zero dollars and zero cents"},
		{"whole dollar", 500, "five dollars and zero cents"},
		{"one hundred", 10000, "one hundred dollars and zero cents"},
		{"hyphenated tens", 12345, "one hundred twenty-three dollars and forty-five cents"},
		{"one thousand", 100000, "one thousand dollars and zero cents"},
		{"thousand plus", 123456, "one thousand two hundred thirty-four dollars and fifty-six cents"},
		{"million scale", 100000000, "one million dollars and zero cents"},
		{"million mix", 123456789, "one million two hundred thirty-four thousand five hundred sixty-seven dollars and eighty-nine cents"},
		{"max accepted", 99999999999, "nine hundred ninety-nine million nine hundred ninety-nine thousand nine hundred ninety-nine dollars and ninety-nine cents"},
		{"cents only", 7, "zero dollars and seven cents"},
		{"teen units", 16, "zero dollars and sixteen cents"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Words(tc.cents)
			if err != nil {
				t.Fatalf("Words(%d): %v", tc.cents, err)
			}
			if got != tc.want {
				t.Errorf("Words(%d) = %q, want %q", tc.cents, got, tc.want)
			}
		})
	}
}

func TestWordsRejects(t *testing.T) {
	if _, err := Words(-1); !errors.Is(err, ErrNegative) {
		t.Errorf("Words(-1) err = %v, want ErrNegative", err)
	}
	if _, err := Words(100000000000); !errors.Is(err, ErrTooLarge) {
		t.Errorf("Words(100000000000) err = %v, want ErrTooLarge", err)
	}
}
