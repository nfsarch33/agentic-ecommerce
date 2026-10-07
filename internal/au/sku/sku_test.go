// Package sku normalises product SKUs and builds URL slugs from product
// titles, the two identifier spellings a WooCommerce ops toolkit handles.
//
// Contract for the implementer (write sku.go, package sku, standard library
// only; declare the sentinel with errors.New):
//
//	var ErrEmpty = errors.New("sku: empty")
//
//	func NormaliseSKU(raw string) (string, error)
//	  - trim surrounding whitespace
//	  - uppercase ASCII letters (other bytes pass through the filter below)
//	  - keep only ASCII letters, digits and '-'; drop every other byte
//	  - collapse runs of '-' to a single '-'
//	  - trim leading and trailing '-'
//	  - return ErrEmpty when the result is empty
//
//	func Slug(raw string) (string, error)
//	  - trim surrounding whitespace
//	  - lowercase ASCII letters (other bytes pass through the filter below)
//	  - keep only ASCII letters, digits and '-'; drop every other byte,
//	    including spaces: "Red Widget XL" -> "red-widget-xl" requires the
//	    space to become '-' BEFORE the filter, not be dropped
//	  - collapse runs of '-' to a single '-'
//	  - trim leading and trailing '-'
//	  - return ErrEmpty when the result is empty
package sku

import (
	"errors"
	"testing"
)

func TestNormaliseSKU(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"lowercase to upper", "abc123", "ABC123"},
		{"keeps digits and hyphen", "AB-1234", "AB-1234"},
		{"strips spaces", " AB 123 ", "AB123"},
		{"strips punctuation", "AB/12_34.X", "AB1234X"},
		{"collapses hyphen runs", "A--B---C", "A-B-C"},
		{"trims hyphens", "-AB-123-", "AB-123"},
		{"unicode letters are dropped", "Über-42", "BER-42"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := NormaliseSKU(tc.in)
			if err != nil {
				t.Fatalf("NormaliseSKU(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("NormaliseSKU(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestNormaliseSKUEmpty(t *testing.T) {
	for _, in := range []string{"", "   ", "---", "///"} {
		if _, err := NormaliseSKU(in); !errors.Is(err, ErrEmpty) {
			t.Errorf("NormaliseSKU(%q) err = %v, want ErrEmpty", in, err)
		}
	}
}

func TestSlug(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"spaces become hyphens", "Red Widget XL", "red-widget-xl"},
		{"lowercases", "Hello-WORLD", "hello-world"},
		{"collapses runs", "  A -- B  ", "a-b"},
		{"trims hyphens", "--red widget--", "red-widget"},
		{"digits kept", "Widget 42 (blue)", "widget-42-blue"},
		{"ampersand dropped", "Tom & Jerry", "tom-jerry"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Slug(tc.in)
			if err != nil {
				t.Fatalf("Slug(%q): %v", tc.in, err)
			}
			if got != tc.want {
				t.Errorf("Slug(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestSlugEmpty(t *testing.T) {
	if _, err := Slug("   ---   "); !errors.Is(err, ErrEmpty) {
		t.Errorf("Slug(spaces/hyphens) err = %v, want ErrEmpty", err)
	}
}

// every Unicode space is a separator — tabs, newlines
// and NBSP (common in WooCommerce titles) hyphenate like ASCII spaces.
func TestSlugUnicodeSpacesHyphenate(t *testing.T) {
	got, err := Slug("Red\u00A0Widget\tXL\n2")
	if err != nil {
		t.Fatal(err)
	}
	if got != "red-widget-xl-2" {
		t.Fatalf("Slug = %q, want red-widget-xl-2", got)
	}
}

// The rune loop must drop non-ASCII runes exactly as the byte filter did:
// a stroke-L and acute-z title keeps its main-branch output (the low-byte
// cast turned Ł into A and ź into z, silently changing existing slugs).
func TestSlugNonASCIIDroppedNotTransliterated(t *testing.T) {
	const title = "Łódź Mug" // stroke-L, acute-z
	if got, err := Slug(title); err != nil || got != "d-mug" {
		t.Fatalf("Slug = %q, %v; want d-mug (main-branch behaviour)", got, err)
	}
	if got, err := NormaliseSKU(title); err != nil || got != "DMUG" {
		t.Fatalf("NormaliseSKU = %q, %v; want DMUG (main-branch behaviour)", got, err)
	}
}
